// Package skillsfs embeds the built-in Agent Skills catalog into the CLI binary.
// The catalog lives at tools/cli/internal/skillsfs/skills/ in the repository.
// The .agents/skills/ directory at the repository root is a symlink to this location.
package skillsfs

import (
	"embed"
	"io/fs"
)

//go:embed skills
var catalog embed.FS

// Catalog returns an fs.FS rooted at the embedded skills directory.
// Each sub-entry is a skill directory named by the skill's `name` field.
func Catalog() fs.FS {
	sub, err := fs.Sub(catalog, "skills")
	if err != nil {
		panic("skillsfs: failed to sub into skills: " + err.Error())
	}
	return sub
}
