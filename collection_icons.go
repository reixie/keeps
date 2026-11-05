package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed resources/collections/reicon/*.svg
var collectionAssets embed.FS

type collectionIconEntry struct {
	key, label string
	icon       *ui.SVG
}

func loadCollectionIcon(key, label string) collectionIconEntry {
	data, err := collectionAssets.ReadFile("resources/collections/reicon/" + key + ".svg")
	if err != nil {
		panic(err)
	}
	return collectionIconEntry{key: key, label: label, icon: ui.MustParseSVG(data)}
}

var collectionIcons = []collectionIconEntry{
	loadCollectionIcon("folder", "Folder"),
	loadCollectionIcon("archive", "Archive"),
	loadCollectionIcon("image", "Image"),
	loadCollectionIcon("video", "Video"),
	loadCollectionIcon("book", "Book"),
	loadCollectionIcon("globe", "Globe"),
	loadCollectionIcon("camera", "Camera"),
	loadCollectionIcon("palette", "Palette"),
	loadCollectionIcon("lightbulb", "Lightbulb"),
	loadCollectionIcon("heart", "Heart"),
	loadCollectionIcon("star", "Star"),
	loadCollectionIcon("bookmark", "Bookmark"),
}

//go:embed resources/collections/plus.svg
var collectionPlusSVG []byte

var collectionPlusIcon = ui.MustParseSVG(collectionPlusSVG)

func collectionIconFor(key string) collectionIconEntry {
	for _, item := range collectionIcons {
		if item.key == key {
			return item
		}
	}
	return collectionIcons[0]
}
