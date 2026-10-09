//go:build retty_cli

package library

// CLI archives carry libghostty-vt next to the executable. Source builds may
// also use the existing hash-verified cache/download path.
func resourceDirs() []string { return nil }
func packaged() bool         { return false }
