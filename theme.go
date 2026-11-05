package main

import "github.com/egoist/mygo/ui"

// Figma Colors collection, Light/Dark modes: Backgrounds/Primary,
// Backgrounds/Translucent, Backgrounds/Secondary, Borders/Border 1,
// Text & Icons/* and Interactions/* consumed by the interface.
type themeValues struct {
	BackgroundPrimary     ui.Color
	BackgroundSecondary   ui.Color
	Border                ui.Color
	ImageStroke           ui.Color
	BackgroundTranslucent ui.Color
	TextPrimary           ui.Color
	TextSecondary         ui.Color
	TextTertiary          ui.Color
	Hover                 ui.Color
	Selected              ui.Color
}

var (
	lightTheme = themeValues{
		BackgroundPrimary:     ui.Hex("#FAFAFA"),
		BackgroundSecondary:   ui.Hex("#FFFFFF"),
		Border:                ui.Hex("#E3E3E3"),
		ImageStroke:           ui.RGBA(0, 0, 0, 0.10),
		BackgroundTranslucent: ui.Hex("#FAFAFA"),
		TextPrimary:           ui.Hex("#000000"),
		TextSecondary:         ui.Hex("#7D7D7D"),
		TextTertiary:          ui.Hex("#A8A8A8"),
		Hover:                 ui.RGBA(0, 0, 0, 0.04),
		Selected:              ui.RGBA(0, 0, 0, 0.06),
	}
	darkTheme = themeValues{
		BackgroundPrimary:     ui.Hex("#0F0F0F"),
		BackgroundSecondary:   ui.Hex("#1A1A1A"),
		Border:                ui.Hex("#242424"),
		ImageStroke:           ui.RGBA(255, 255, 255, 0.10),
		BackgroundTranslucent: ui.Hex("#050505"),
		TextPrimary:           ui.Hex("#FFFFFF"),
		TextSecondary:         ui.Hex("#8F8E8E"),
		TextTertiary:          ui.Hex("#616161"),
		Hover:                 ui.RGBA(255, 255, 255, 0.08),
		Selected:              ui.RGBA(255, 255, 255, 0.12),
	}
)

func themeFor(dark bool) themeValues {
	if dark {
		return darkTheme
	}
	return lightTheme
}
