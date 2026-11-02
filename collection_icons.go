package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed resources/collections/*.svg
var collectionAssets embed.FS

func collectionAsset(key string) *ui.SVG {
	data, err := collectionAssets.ReadFile("resources/collections/" + key + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var collectionIcons = []struct {
	key, label string
	icon       *ui.SVG
}{
	{"folder", "Folder", collectionAsset("folder")},
	{"archive", "Archive", collectionAsset("archive")},
	{"image", "Image", collectionAsset("image")},
	{"video", "Video", collectionAsset("video")},
	{"book", "Book", collectionAsset("book")},
	{"globe", "Globe", collectionAsset("globe")},
	{"camera", "Camera", collectionAsset("camera")},
	{"palette", "Palette", collectionAsset("palette")},
	{"lightbulb", "Lightbulb", collectionAsset("lightbulb")},
	{"heart", "Heart", collectionAsset("heart")},
	{"star", "Star", collectionAsset("star")},
	{"bookmark", "Bookmark", collectionAsset("bookmark")},
}

var collectionPlusIcon = collectionAsset("plus")

func collectionIcon(key string) *ui.SVG {
	for _, item := range collectionIcons {
		if item.key == key {
			return item.icon
		}
	}
	return collectionIcons[0].icon
}

func collectionIconLabel(key string) string {
	for _, item := range collectionIcons {
		if item.key == key {
			return item.label
		}
	}
	return "Folder"
}
