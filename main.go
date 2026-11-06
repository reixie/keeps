package main

import (
	"context"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	referenceWidth      = 1200
	referenceHeight     = 800
	sidebarWidth        = 248
	contentPadding      = 4
	panelRadius         = 12
	dragHeight          = 52
	sidebarControlSize  = 32
	sidebarControlGap   = 4
	sidebarControlTop   = 12
	sidebarControlInset = 12
	panelBorderWidth    = 1
)

type app struct {
	reduceTransparency       bool
	navigation               navigationState
	focus                    string
	sidebarMenu              *mygo.MenuItem
	window                   *mygo.Window
	captureMenu              *macCaptureMenu
	store                    *libraryStore
	items                    []capture
	collections              []collection
	collectionDialog         *collectionDialogState
	linkDialog               *linkDialogState
	cardMenu                 *captureMenuSnapshot
	contextSidebarCollection collection
	loading                  bool
	loadError                error
	announcements            []string
	thumbnails               *thumbnailCache
	grid                     captureGrid
	selection                captureSelection
	libraryJobs              []libraryJob
	libraryJobRunning        bool
	pasteQueue               []pasteRequest
	pasteCancel              context.CancelFunc
	picking                  bool
	nativeDialogs            int
	copyGeneration           uint64
	pointerActivation        string
	cleanupQueued            bool
	closed                   bool
	stopRetention            func()
	cleanupError             string
	reduceMotion             bool
	media                    *mediaController
	purging                  bool
	preferences              appPreferences
	preferencesError         error
	libraryOverride          bool
	defaultRoot              string
	pendingSpace             *spaceRequest
	switchingSpace           bool
	spaceGeneration          uint64
	tags                     []libraryTag
	tagEditor                *tagEditorState
	filters                  captureFilters
	browse                   browseState
	controls                 *nativeLibraryControls
	settingsSpaceID          string
	settingsSpaceError       string
	checkingUpdates          bool
	updateStatus             string
	releaseURL               string
}

func (a *app) view(c *ui.Context) {
	for _, text := range a.announcements {
		c.Announce(text)
	}
	a.announcements = nil
	t := themeFor(c.Theme().Dark)
	widgetTheme := *c.Theme()
	// Keep the system focus hue and Mygo's keyboard-only ring geometry,
	// but avoid fading the indicator into the translucent sidebar.
	widgetTheme.Focus.A = 255
	widgetTheme.FontSize = 13
	widgetTheme.Font = "SF Pro, system-ui"
	widgetTheme.Text = t.TextSecondary
	widgetTheme.Surface = ui.Transparent
	widgetTheme.SurfaceHover = t.Hover
	widgetTheme.SurfacePressed = t.Selected
	widgetTheme.Radius = 8
	c.SetTheme(&widgetTheme)
	c.Root().Background(ui.Transparent)
	background := ui.Transparent
	if !a.preferences.Translucent || a.reduceTransparency || !c.Vibrancy() {
		background = t.BackgroundTranslucent
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(background).Children(func() {
		if !a.navigation.hidden {
			a.sidebar(c, t)
		}
		ui.Column(c).Grow(1).MinWidth(0).Padding(contentPadding).Children(func() {
			a.libraryView(c, t)
		})
	})
	// The content Header owns its drag region. These handles cover only the
	// sidebar/native-control band, never the Header's action buttons.
	toggleX := a.sidebarToggleX(c)
	controlsLeft := toggleX
	if !a.navigation.hidden {
		controlsLeft -= sidebarControlSize + sidebarControlGap
	}
	ui.Box(c).Absolute().Left(0).Top(0).Width(controlsLeft).Height(dragHeight).DragWindow()
	if !a.navigation.hidden {
		ui.Box(c).Absolute().Left(toggleX + sidebarControlSize).Top(0).Width(sidebarWidth - toggleX - sidebarControlSize).Height(dragHeight).DragWindow()
	}
	if !a.navigation.hidden {
		ui.Box(c).Absolute().Left(controlsLeft).Top(sidebarControlTop).
			Size(2*sidebarControlSize+sidebarControlGap, sidebarControlSize).Radius(sidebarControlSize / 2).
			Background(t.Hover)
		a.sidebarAddCapture(c, t, controlsLeft)
	}
	a.sidebarToggle(c, t, toggleX)
	a.collectionsModal(c, t)
	a.linkModal(c, t)
	// SetTheme also updates the root; per-row theme adaptation must leave
	// the native material visible after the whole view has been built.
	c.Root().Background(ui.Transparent)
}

func main() {
	a := newApp()
	mygo.App.SetName("Keeps")
	mygo.App.WhenReady(func() {
		a.libraryOverride = os.Getenv("KEEPS_LIBRARY") != ""
		a.preferences, a.preferencesError = loadPreferences()
		mygo.Theme.SetSource(a.preferences.Appearance)
		a.reduceTransparency = mygo.Theme.Preferences().ReduceTransparency
		a.reduceMotion = mygo.Theme.Preferences().ReduceMotion
		work := mygo.Screen.PrimaryDisplay().WorkArea
		width, height := min(referenceWidth, work.Width), min(referenceHeight, work.Height)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "Keeps",
			Width:          width,
			Height:         height,
			Hidden:         true,
			MinWidth:       min(520, width),
			MinHeight:      min(220, height),
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: dragHeight,
			Transparent:    true,
			Vibrancy:       mygo.VibrancySidebar,
			Content:        ui.View(a.view),
		})
		// SetBounds also handles (0, 0), which WindowOptions reserves for
		// centering on the screen rather than its available work area.
		win.SetBounds(mygo.Rectangle{
			X: work.X + (work.Width-width)/2, Y: work.Y + (work.Height-height)/2,
			Width: width, Height: height,
		})
		followWindowActivity(win)
		a.window = win
		menu, err := newMacCaptureMenu(win.NativeHandle(), a.captureMenuClosed)
		if err != nil {
			log.Printf("Native capture menu unavailable: %v", err)
		}
		a.captureMenu = menu
		controls, err := newLibraryControls(win.NativeHandle(), a.nativeControlAction)
		if err != nil {
			log.Printf("Native library controls unavailable: %v", err)
		}
		a.controls = controls
		if a.preferencesError != nil {
			a.report("Could not load settings", a.preferencesError.Error(), "")
		}
		if backend, err := newMacPlayback(win.NativeHandle()); err != nil {
			log.Printf("Native playback unavailable: %v", err)
			const title = "Native viewing could not start"
			a.announce(title)
			a.report(title, err.Error(), "")
		} else {
			a.startPlayback(backend)
		}
		a.installMenu()
		off := mygo.Theme.OnUpdated(func() {
			a.reduceTransparency = mygo.Theme.Preferences().ReduceTransparency
			a.reduceMotion = mygo.Theme.Preferences().ReduceMotion
			if a.reduceMotion && a.media != nil {
				a.media.releaseInline()
			}
			win.Invalidate()
		})
		win.OnClosed(off)
		win.OnClosed(a.shutdown)
		win.Show()
		a.startLibrary()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
