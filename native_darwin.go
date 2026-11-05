package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
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
