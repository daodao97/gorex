//go:build !ios

package terminal

import "retty/internal/terminal/internal/library/desktop"

var natives = desktop.Manifest
