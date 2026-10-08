package terminal

import (
	"fmt"
	"runtime"
	"sync"

	"gorex/internal/terminal/internal/library"
	"gorex/internal/terminal/internal/vt"
)

var load struct {
	once sync.Once
	path string
	err  error
}

// Load loads libghostty-vt, once, from LibraryPath. New calls it; call it
// earlier to report a missing library before showing a terminal.
func Load() error {
	load.once.Do(func() {
		if !vt.Supported {
			load.err = fmt.Errorf("terminal: not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
			return
		}
		if runtime.GOOS == "ios" {
			load.err = vt.Load("linked")
			return
		}
		load.path, load.err = LibraryPath()
		if load.err == nil {
			load.err = vt.Load(load.path)
		}
	})
	return load.err
}

// LibraryPath returns the libghostty-vt the terminal loads, the first of:
//
//   - the file $MYGO_GHOSTTY_VT names;
//   - the one among the app's resources (mygo.PathResources), where `mygo
//     build` and `mygo dev` put it, signed with the app on macOS;
//   - the one next to the executable;
//   - the one in the user's cache, where the CLI downloads it
//     (<cache>/mygo/natives/<sha256>/), which programs that are not
//     packaged apps, as under `go run` and `go test`, download there when
//     it is missing, checking its SHA-256.
//
// Packaged apps never download it: build them with the CLI.
func LibraryPath() (string, error) { return library.Find(natives) }
