package main

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/egoist/mygo/ui"
)

func decodePreview(data []byte) (*ui.Bitmap, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 {
		return nil, fmt.Errorf("invalid preview dimensions")
	}
	return ui.DecodeBitmap(data)
}
