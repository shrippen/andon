//go:build release

package demoworld

import _ "embed"

// The neutral sample world (demo/make-sample.py derives it from
// world.json): release builds preview gallery tiles without Studio Weber.
//
//go:embed sample.json
var raw []byte
