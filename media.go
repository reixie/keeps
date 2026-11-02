package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

type mediaInfo struct {
	Type   string `json:"type"`
	Format string `json:"format"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type mediaAdapter func(original, preview string) (mediaInfo, error)

var nativeMediaSlots = make(chan struct{}, 2)

func nativeHelper() string {
	dir, _ := mygo.App.Path(mygo.PathResources)
	path := filepath.Join(dir, "KeepsMedia")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	// Direct development runs use resources without platform merging.
	return filepath.Join(dir, "darwin-"+hostArch(), "KeepsMedia")
}

func nativePreview(original, preview string) (mediaInfo, error) {
	nativeMediaSlots <- struct{}{}
	defer func() { <-nativeMediaSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nativeHelper(), original, preview)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		log.Printf("media preview: %v: %s", err, strings.TrimSpace(stderr.String()))
		if ctx.Err() != nil {
			return mediaInfo{}, fmt.Errorf("preview generation timed out")
		}
		return mediaInfo{}, fmt.Errorf("unsupported, damaged, or unreadable media")
	}
	var info mediaInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return info, err
	}
	if !validMediaInfo(info) {
		return info, fmt.Errorf("invalid media dimensions or format")
	}
	return info, nil
}

func validMediaInfo(info mediaInfo) bool {
	return info.Width > 0 && info.Height > 0 && info.Width <= 1_000_000 && info.Height <= 1_000_000 &&
		((info.Type == "image" && (info.Format == "jpg" || info.Format == "png" || info.Format == "webp" || info.Format == "gif")) ||
			(info.Type == "video" && (info.Format == "mp4" || info.Format == "mov")))
}

func defaultLibraryRoot() (string, error) {
	// A separate library can be selected for local development.
	if root := os.Getenv("KEEPS_LIBRARY"); root != "" {
		return filepath.Abs(root)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, nativeHelper(), "--documents").Output()
	if err != nil {
		return "", fmt.Errorf("locate system Documents folder: %w", err)
	}
	root := strings.TrimSpace(string(out))
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("system Documents folder is unavailable")
	}
	return filepath.Join(root, "Keeps Library"), nil
}

// Conversion is called by the clipboard worker, never a UI/provider callback.
func originalPNG(path string) ([]byte, error) {
	nativeMediaSlots <- struct{}{}
	defer func() { <-nativeMediaSlots }()
	dir, err := os.MkdirTemp("", "keeps-copy-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	output := filepath.Join(dir, "image.png")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, nativeHelper(), "--raster", path, output).Run(); err != nil {
		return nil, fmt.Errorf("could not prepare original image for clipboard: %w", err)
	}
	return os.ReadFile(output)
}
