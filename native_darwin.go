package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

func nativeResource(name string) (string, error) {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return "", fmt.Errorf("locate Keeps resources: %w", err)
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// Development runs use resources without platform merging.
		return filepath.Join(dir, "darwin-"+runtime.GOARCH, name), nil
	}
	return path, nil
}

// AVFoundation cancellation completions can still be queued after the last
// window closes. Keep the Swift dylib loaded for the life of the process.
var openKeepsNative = sync.OnceValues(func() (uintptr, error) {
	path, err := nativeResource("libKeepsPlayback.dylib")
	if err != nil {
		return 0, err
	}
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
})

func bindKeepsNative(symbols map[string]any) error {
	lib, err := openKeepsNative()
	if err != nil {
		return fmt.Errorf("load Keeps native library: %w", err)
	}
	for name, fn := range symbols {
		address, err := purego.Dlsym(lib, name)
		if err != nil {
			return fmt.Errorf("Keeps native ABI %s: %w", name, err)
		}
		purego.RegisterFunc(fn, address)
	}
	return nil
}

func nativeNSString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), s)
}

func nativeString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	n := uintptr(s.Send(objc.RegisterName("lengthOfBytesUsingEncoding:"), uintptr(4))) // NSUTF8StringEncoding
	if n == 0 || n > 64<<20 {
		return ""
	}
	p := objc.Send[unsafe.Pointer](s, objc.RegisterName("UTF8String"))
	if p == nil {
		return ""
	}
	return string(unsafe.Slice((*byte)(p), int(n)))
}
