// Package assets embeds the skill and rule templates installed by `bound init`.
package assets

import "embed"

// FS holds skill/SKILL.md and AGENTS.snippet.md.
//
//go:embed skill/SKILL.md AGENTS.snippet.md
var FS embed.FS
