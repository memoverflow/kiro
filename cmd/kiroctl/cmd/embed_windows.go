//go:build windows

package cmd

import _ "embed"

//go:embed embed/sing-box-windows-amd64.exe
var embeddedSingBox []byte

//go:embed embed/sing-box.version
var embeddedSingBoxVersion string
