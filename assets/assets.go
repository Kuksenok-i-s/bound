// Package assets embeds the skill and rule templates installed by `bound init`.
package assets

import "embed"

// FS holds the skills and the AGENTS.md snippet.
//
//go:embed skill/SKILL.md skill-proto/SKILL.md AGENTS.snippet.md
var FS embed.FS

// Skills maps embedded skill files to the directory name they are installed under.
var Skills = map[string]string{
	"skill/SKILL.md":       "bound",
	"skill-proto/SKILL.md": "proto",
}
