// Package scripts embeds the installer scripts. jd runs them itself on its first start when it was installed with pip, so
// that pip install does what `curl ... | sh` does: install the tools the IDE needs and make jd git's diff and merge tool.
package scripts

import _ "embed"

// InstallSh is install.sh (Linux, macOS).
//
//go:embed install.sh
var InstallSh []byte

// InstallPs1 is install.ps1 (Windows).
//
//go:embed install.ps1
var InstallPs1 []byte
