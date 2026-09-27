//go:build !release

package demoworld

import _ "embed"

// Studio Weber: demo mode, screenshots and tests.
//
//go:embed world.json
var raw []byte
