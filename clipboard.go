package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"golang.org/x/net/idna"
)

type pasteInput struct {
	paths    []string
	urls     []string
	failures []importFailure
	cleanup  func()
}

// Prefer files per logical item, not globally. A missing/unreadable local
// file remains a file failure; its raster alternative must not become a second
// capture. No decoding, copying or filesystem work occurs during drag hover.
func pastedInputs(data transfer.Data) (input pasteInput) {
	var temp string
	seen := map[string]bool{}
	addURL := func(raw, label string, native bool) {
		normalized, err := clipboardLink(raw, native)
		if err != nil {
			input.failures = append(input.failures, importFailure{label, err.Error()})
		} else if !seen[normalized] {
			seen[normalized] = true
			input.urls = append(input.urls, normalized)
		}
	}
	input.cleanup = func() {
		if temp != "" {
			_ = os.RemoveAll(temp)
		}
	}
	for i, item := range data.Items() {
		one := transfer.New(item)
		formats := item.Formats()
		paths, fileErr := one.Files()
		if fileErr != nil && !errors.Is(fileErr, transfer.ErrFormat) && slices.Contains(formats, transfer.FileList) {
			input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard item %d", i+1), fileErr.Error()})
			continue
		}
		if len(paths) > 0 {
			input.paths = append(input.paths, paths...)
			continue
		}
		if !slices.Contains(formats, transfer.PNG) {
			if fileErr != nil && !errors.Is(fileErr, transfer.ErrFormat) {
				input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard item %d", i+1), fileErr.Error()})
				continue
			}
			if slices.Contains(formats, transfer.FileList) {
				input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard item %d", i+1), "No valid local file URLs. Copy the original local files again."})
				continue
			}
			if slices.Contains(formats, transfer.URIList) {
				urls, readErr := one.URLs()
				if readErr != nil {
					input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard URL item %d", i+1), readErr.Error()})
					continue
				}
				if len(urls) > 0 {
					for n, raw := range urls {
						addURL(raw, fmt.Sprintf("Clipboard URL item %d, address %d", i+1, n+1), true)
					}
					continue // Text is an alternative of this same logical item.
				}
			}
			if slices.Contains(formats, transfer.Text) {
				raw, readErr := item.Read(transfer.Text)
				if readErr != nil {
					input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard text item %d", i+1), readErr.Error()})
					continue
				}
				lines := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
				for n, line := range strings.Split(lines, "\n") {
					if line = strings.TrimSpace(line); line != "" {
						addURL(line, fmt.Sprintf("Clipboard text item %d, line %d", i+1, n+1), false)
					}
				}
			}
			continue
		}
		raw, readErr := item.Read(transfer.PNG)
		if readErr == nil {
			// Validate the PNG header off the UI thread; the importer inspects the full
			// content. Preserve its original encoded bytes,
			// dimensions and alpha rather than reencoding through a preview.
			_, readErr = png.DecodeConfig(bytes.NewReader(raw))
		}
		if readErr != nil {
			input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard image %d", i+1), "Unreadable image"})
			continue
		}
		if temp == "" {
			var err error
			temp, err = os.MkdirTemp("", "keeps-paste-")
			if err != nil {
				input.failures = append(input.failures, importFailure{fmt.Sprintf("Clipboard image %d", i+1), err.Error()})
				continue
			}
		}
		path := filepath.Join(temp, fmt.Sprintf("Pasted image %s-%d.png", time.Now().Format("2006-01-02 15.04.05"), i+1))
		if err := os.WriteFile(path, raw, 0600); err != nil {
			input.failures = append(input.failures, importFailure{filepath.Base(path), err.Error()})
			continue
		}
		input.paths = append(input.paths, path)
	}
	return input
}

// The Add Link dialog deliberately permits intranet hostnames. Clipboard text
// needs stronger evidence of an address so ordinary words never become links.
func clipboardLink(raw string, native bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("Use one complete HTTP/HTTPS URL or address per line; links are not extracted from prose.")
	}
	lower := strings.ToLower(raw)
	explicit := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
	if native && !explicit {
		return "", fmt.Errorf("Only HTTP and HTTPS links can be saved. Copy a local file to import media.")
	}
	if !explicit {
		if ip, err := netip.ParseAddr(raw); err == nil && ip.Is6() {
			raw = "[" + raw + "]"
		}
	}
	normalized, err := normalizeLink(raw)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(normalized)
	if !explicit && !clearClipboardHost(u.Hostname()) {
		return "", fmt.Errorf("Use a complete HTTP/HTTPS URL, a domain such as example.com, an IP address or localhost.")
	}
	return normalized, nil
}

func clearClipboardHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(host, "."))
	if err != nil || len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	return len(tld) >= 2 && strings.ContainsFunc(tld, unicode.IsLetter)
}

func (a *app) pasteMedia() {
	if a.libraryInputBlocked() || a.loadError != nil || a.loading || a.store == nil {
		return
	}
	// Read now, in response to this Paste. Mygo snapshots item boundaries and
	// detects changes of ownership; queued work never rereads a different board.
	destination := a.activeCollectionID()
	data, err := readCaptureClipboard()
	if err != nil {
		a.announce("Clipboard has no readable media or links")
		if !errors.Is(err, transfer.ErrFormat) {
			a.report("Could not paste", err.Error(), "Copy readable media or HTTP/HTTPS links, then try Paste again.")
		}
		return
	}
	if len(data.Items()) == 0 {
		a.announce("Clipboard has no media or links")
		return
	}
	a.pasteQueue = append(a.pasteQueue, pasteRequest{data: data, destination: destination})
	a.startNextPaste()
}

func copyCaptures(root string, items []capture) (transfer.Data, error) {
	var out []transfer.Item
	for _, item := range items {
		if item.isLink() {
			data, err := transfer.URLData(item.URL)
			if err != nil {
				return transfer.Data{}, err
			}
			out = append(out, data.Items()...)
			continue
		}
		path := filepath.Join(root, item.Original)
		stat, err := os.Stat(path)
		if err != nil {
			return transfer.Data{}, err
		}
		if !stat.Mode().IsRegular() {
			return transfer.Data{}, fmt.Errorf("original is not a file: %s", item.Filename)
		}
		data, err := transfer.FileData(path)
		if err != nil {
			return transfer.Data{}, err
		}
		file := data.Items()[0]
		var reps []transfer.Representation
		for _, f := range file.Formats() {
			b, _ := file.Read(f)
			reps = append(reps, transfer.Bytes(f, b))
		}
		if len(items) == 1 && item.MediaType == "image" {
			raw, err := originalPNG(path)
			if err != nil {
				return transfer.Data{}, err
			}
			reps = append(reps, transfer.Bytes(transfer.PNG, raw))
		}
		out = append(out, transfer.NewItem(reps...))
	}
	return transfer.New(out...), nil
}

func (a *app) copySelected() {
	a.copyGeneration++
	if a.libraryInputBlocked() || !a.captureSection() || a.store == nil {
		return
	}
	selected := a.selection.orderedCopy(a.visibleItems())
	if len(selected) == 0 {
		return
	} // In particular, never Write(empty).
	generation, root := a.copyGeneration, a.store.root
	go func() {
		data, err := copyCaptures(root, selected)
		a.window.Update(func() {
			if generation != a.copyGeneration || a.libraryInputBlocked() {
				return
			}
			if err == nil {
				err = mygo.Clipboard.Write(data)
				if err == nil {
					err = mygo.Clipboard.Flush()
				}
			}
			if err != nil {
				a.report("Could not copy", err.Error(), "Try Copy again. If it still fails, quit and reopen Keeps.")
				a.announce("Could not copy")
				return
			}
			a.announce(fmt.Sprintf("%d copied", len(selected)))
		})
	}()
}
