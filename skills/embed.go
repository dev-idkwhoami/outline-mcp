// Package skills embeds the distributable skill in both platform binaries.
package skills

import _ "embed"

//go:embed outline/SKILL.md
var Outline []byte
