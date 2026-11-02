package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const (
	referenceWidth  = 1200
	referenceHeight = 800
	sidebarWidth    = 248
	contentPadding  = 4
	panelRadius     = 12
	dragHeight      = 52
)

type app struct {
	reduceTransparency       bool
	navigation               navigationState
	focus                    string
	sidebarMenu              *mygo.MenuItem
	invalidate               func()
	window                   *mygo.Window
	update                   func(func())
	pickMedia                func() ([]string, error)
	confirmPermanentDeletion func(string) (bool, error)
	store                    *libraryStore
	items                    []capture
	collections              []collection
	collectionDialog         *collectionDialogState
	linkDialog               *linkDialogState
	contextCollection        string
	contextCollections       []collectionMenuEntry
	contextSidebarCollection collection
	loading, importing       bool
	loadError                error
	// Completion text is used only for accessibility announcements.
	status            string
	announcements     []string
	thumbnails        *thumbnailCache
	libraryScroll     ui.ScrollState
	layout            masonryLayout
	layoutWidth       float32
	layoutItems       []capture
	selection         captureSelection
	selectionOrder    []string
	importQueue       []importBatch
	pasteQueue        []pasteRequest
	pasteWork         *pasteWork
	picking           bool
	nativeDialogs     int
	copyGeneration    uint64
	readClipboard     func() (transfer.Data, error)
	writeClipboard    func(transfer.Data) error
	rasterOriginal    func(string) ([]byte, error)
	pointerActivation string
	contextCard       string
	contextAction     starredOperation
	contextTrash      trashOperation
	focusNeedsReveal  bool
	cleanupQueued     bool
	closed            bool
	stopRetention     func()
	cleanupError      string
	reduceMotion      bool
	media             *mediaController
	purging           bool
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
	background := t.BackgroundTranslucent.Alpha(materialTintStrength)
	if a.reduceTransparency || !c.Vibrancy() {
		background = t.BackgroundTranslucent
		background.A = 255
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
		controlsLeft -= 36 // 32-point Add capture, then the four-point gap.
	}
	ui.Box(c).Absolute().Left(0).Top(0).Width(controlsLeft).Height(dragHeight).DragWindow()
	if !a.navigation.hidden {
		ui.Box(c).Absolute().Left(toggleX + 32).Top(0).Width(sidebarWidth - toggleX - 32).Height(dragHeight).DragWindow()
	}
	if !a.navigation.hidden {
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
	mygo.Theme.SetSource(mygo.ThemeSystem)
	mygo.App.WhenReady(func() {
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
		a.update = win.Update
		a.pickMedia = func() ([]string, error) {
			// Let readable bytes decide support, including media with misleading
			// suffixes. The importer reports unsupported files per item.
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "Add capture", ButtonLabel: "Import", Multiple: true})
		}
		a.confirmPermanentDeletion = func(message string) (bool, error) {
			result, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: win, Type: mygo.MessageWarning,
				Title: "Delete Permanently", Message: message, Detail: "This can’t be undone.",
				Buttons: []string{"Cancel", "Delete Permanently"}, DefaultButton: 0, CancelButton: 0})
			return result.Button == 1, err
		}
		if backend, err := newMacPlayback(win.NativeHandle()); err != nil {
			log.Printf("Native playback unavailable: %v", err)
			a.status = "Native viewing could not start"
			a.announce(a.status)
			a.report(a.status, err.Error())
		} else {
			a.startPlayback(backend)
		}
		a.invalidate = win.Invalidate
		a.readClipboard = readCaptureClipboard
		a.writeClipboard = func(data transfer.Data) error {
			if err := mygo.Clipboard.Write(data); err != nil {
				return err
			}
			return mygo.Clipboard.Flush()
		}
		a.rasterOriginal = originalPNG
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
