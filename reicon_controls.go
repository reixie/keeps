package main

import (
	"embed"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/reicon-controls/*.svg resources/reicon-controls/*.png
var reiconControls embed.FS

func reiconAsset(name, suffix string) []byte {
	data, err := reiconControls.ReadFile("resources/reicon-controls/" + name + suffix)
	if err != nil {
		panic(err)
	}
	return data
}

func reiconGlyph(name string) *ui.SVG {
	return ui.MustParseSVG(reiconAsset(name, ".svg"))
}

func reiconMenuGlyph(name string) *mygo.MenuIcon {
	icon, err := mygo.NewMenuIcon(reiconAsset(name, ".png"), true)
	if err != nil {
		panic(err)
	}
	return icon
}
