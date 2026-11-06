package main

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

var captureClipboardFormats = []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}

// Called only by the Paste handler on Mygo's main thread. Normally Mygo does
// all native snapshotting. Its reader aborts the whole transfer on one
// oversized representation or failed TIFF conversion; that case needs a
// per-item snapshot so unrelated files/links can still be saved.
func readCaptureClipboard() (transfer.Data, error) {
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	board := objc.ID(objc.GetClass("NSPasteboard")).Send(objc.RegisterName("generalPasteboard"))
	change := board.Send(objc.RegisterName("changeCount"))
	data, err := mygo.Clipboard.Read(captureClipboardFormats...)
	if err == nil || errors.Is(err, transfer.ErrFormat) || errors.Is(err, mygo.ErrClipboardChanged) || errors.Is(err, mygo.ErrClipboardReentrant) {
		return data, err
	}
	if board.Send(objc.RegisterName("changeCount")) != change {
		return transfer.Data{}, mygo.ErrClipboardChanged
	}
	var items []transfer.Item
	for _, item := range clipboardArray(board.Send(objc.RegisterName("pasteboardItems"))) {
		available := map[string]bool{}
		for _, typ := range clipboardArray(item.Send(objc.RegisterName("types"))) {
			available[nativeString(typ)] = true
		}
		var reps []transfer.Representation
		for _, group := range []struct {
			format transfer.Format
			names  []string
		}{
			{transfer.FileList, []string{"dev.mygo.file-list", "public.file-url", "NSFilenamesPboardType"}},
			{transfer.PNG, []string{"public.png", "public.tiff"}},
			{transfer.URIList, []string{"dev.mygo.uri-list", "text/uri-list", "public.url", "public.file-url"}},
			{transfer.Text, []string{"public.utf8-plain-text", "public.plain-text", "public.utf16-plain-text", "public.utf16-external-plain-text", "NSStringPboardType"}},
		} {
			for _, name := range group.names {
				if !available[name] {
					continue
				}
				raw, readErr := clipboardRepresentation(item, name, group.format)
				if readErr != nil {
					// Only the immutable error is retained, never a native provider.
					reps = append(reps, transfer.Lazy(group.format, func() ([]byte, error) { return nil, readErr }))
				} else {
					reps = append(reps, transfer.Bytes(group.format, raw))
				}
				break
			}
		}
		if len(reps) > 0 {
			items = append(items, transfer.NewItem(reps...))
		}
	}
	if board.Send(objc.RegisterName("changeCount")) != change {
		return transfer.Data{}, mygo.ErrClipboardChanged
	}
	if len(items) == 0 {
		return transfer.Data{}, err
	}
	return transfer.New(items...), nil
}

func clipboardArray(array objc.ID) []objc.ID {
	var items []objc.ID
	for i, n := uintptr(0), uintptr(array.Send(objc.RegisterName("count"))); i < n; i++ {
		items = append(items, array.Send(objc.RegisterName("objectAtIndex:"), i))
	}
	return items
}

func clipboardRepresentation(item objc.ID, name string, format transfer.Format) ([]byte, error) {
	typ := nativeNSString(name)
	if name == "NSFilenamesPboardType" {
		var paths []string
		for _, value := range clipboardArray(item.Send(objc.RegisterName("propertyListForType:"), typ)) {
			paths = append(paths, nativeString(value))
		}
		files, err := transfer.FileData(paths...)
		if err != nil {
			return nil, err
		}
		return files.Read(transfer.FileList)
	}
	data := item.Send(objc.RegisterName("dataForType:"), typ)
	if data == 0 {
		return nil, fmt.Errorf("Clipboard data is unreadable. Copy this item again.")
	}
	if uintptr(data.Send(objc.RegisterName("length"))) > 64<<20 {
		return nil, fmt.Errorf("Clipboard representation exceeds 64 MiB. Import the original file instead.")
	}
	if format == transfer.Text {
		text := item.Send(objc.RegisterName("stringForType:"), typ)
		if text == 0 || uintptr(text.Send(objc.RegisterName("lengthOfBytesUsingEncoding:"), uintptr(4))) > 64<<20 {
			return nil, fmt.Errorf("Clipboard text is unreadable or too large. Copy the addresses again.")
		}
		return []byte(nativeString(text)), nil
	}
	if name == "public.tiff" {
		image := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(objc.RegisterName("imageRepWithData:"), data)
		if image == 0 {
			return nil, fmt.Errorf("Unreadable image. Copy it again or import the original file.")
		}
		properties := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionary"))
		data = image.Send(objc.RegisterName("representationUsingType:properties:"), uintptr(4), properties) // NSPNGFileType
	}
	if data == 0 {
		return nil, fmt.Errorf("Could not read clipboard image. Import the original file instead.")
	}
	n := uintptr(data.Send(objc.RegisterName("length")))
	if n > 64<<20 {
		return nil, fmt.Errorf("Clipboard representation exceeds 64 MiB. Import the original file instead.")
	}
	var raw []byte
	if n > 0 {
		p := objc.Send[unsafe.Pointer](data, objc.RegisterName("bytes"))
		if p == nil {
			return nil, fmt.Errorf("Clipboard data is unreadable. Copy this item again.")
		}
		raw = append([]byte(nil), unsafe.Slice((*byte)(p), int(n))...)
	}
	if name == "public.file-url" {
		value := strings.TrimRight(string(raw), "\x00")
		if strings.HasPrefix(value, "file:///.file/") {
			u := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("URLWithString:"), nativeNSString(value))
			if resolved := nativeString(u.Send(objc.RegisterName("filePathURL")).Send(objc.RegisterName("absoluteString"))); resolved != "" {
				raw = []byte(resolved)
			}
		}
	}
	return raw, nil
}
