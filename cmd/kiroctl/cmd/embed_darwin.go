//go:build darwin

package cmd

import _ "embed"

//go:embed embed/sing-box-darwin-arm64
var embeddedSingBox []byte

//go:embed embed/sing-box.version
var embeddedSingBoxVersion string
