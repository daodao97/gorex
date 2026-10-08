// Package desktop describes the dynamic libraries packaged on desktop platforms.
package desktop

import _ "embed"

//go:embed mygo-plugin.json
var Manifest []byte
