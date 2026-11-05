package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type mediaInfo struct {
	Type   string `json:"type"`
	Format string `json:"format"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

var nativeMediaSlots = make(chan struct{}, 2)

func nativeMediaCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	path, err := nativeResource("KeepsMedia")
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...), nil
}

func nativeMediaOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd, err := nativeMediaCommand(ctx, args...)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}

func nativePreview(original, preview string) (mediaInfo, error) {
	nativeMediaSlots <- struct{}{}
	defer func() { <-nativeMediaSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd, err := nativeMediaCommand(ctx, original, preview)
	if err != nil {
		return mediaInfo{}, err
	}
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
	out, err := nativeMediaOutput(ctx, "--documents")
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
	cmd, err := nativeMediaCommand(ctx, "--raster", path, output)
	if err != nil {
		return nil, err
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not prepare original image for clipboard: %w", err)
	}
	return os.ReadFile(output)
}

func runLinkMediaCommand(ctx context.Context, args ...string) ([]byte, error) {
	select {
	case nativeMediaSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-nativeMediaSlots }()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := nativeMediaOutput(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("Could not prepare local link images: %w", err)
	}
	return out, nil
}

func nativeLinkImage(ctx context.Context, input, output string, edge int) (mediaInfo, error) {
	out, err := runLinkMediaCommand(ctx, "--link-image", input, output, strconv.Itoa(edge))
	if err != nil {
		return mediaInfo{}, err
	}
	var info mediaInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return mediaInfo{}, err
	}
	if info.Width <= 0 || info.Height <= 0 {
		return mediaInfo{}, fmt.Errorf("invalid link image dimensions")
	}
	return info, nil
}

func nativeLinkPlaceholder(ctx context.Context, output string) error {
	_, err := runLinkMediaCommand(ctx, "--link-placeholder", output)
	return err
}

// Matches LinkComposition in the KeepsMedia helper.
type linkComposition struct {
	Favicon string `json:"favicon"`
	Image   string `json:"image"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

func nativeLinkCompose(ctx context.Context, specPath, output string, spec linkComposition) error {
	data, _ := json.Marshal(spec)
	if err := os.WriteFile(specPath, data, 0600); err != nil {
		return err
	}
	_, err := runLinkMediaCommand(ctx, "--link-compose", specPath, output)
	return err
}
