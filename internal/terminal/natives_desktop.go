//go:build !ios

package terminal

import "gorex/internal/terminal/internal/library/desktop"

var natives = desktop.Manifest
