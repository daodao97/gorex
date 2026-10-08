//go:build ios

package terminal

// iOS links libghostty-vt into the application; executable code is never downloaded.
var natives []byte
