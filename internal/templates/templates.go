// Package templates carries the embedded files copied into a workspace
// by the `init` command: top-level markdown templates and the OpenClaw
// skill files under skills/<name>/SKILL.md.
//
// All files are go:embed'd at compile time. Calling code does not need
// to ship the templates separately and the binary stays self-contained.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"text/template"
)

//go:embed ATHLETE-PROFILE.md.tmpl README.md.tmpl skills
var content embed.FS

// AthleteProfile returns the un-templated ATHLETE-PROFILE.md body.
//
// The template currently has no substitutions; the function exists so
// that future placeholders can be added without changing call sites.
func AthleteProfile() ([]byte, error) {
	return content.ReadFile("ATHLETE-PROFILE.md.tmpl")
}

// Readme returns the README.md body with vars substituted.
//
// vars must include at least:
//
//   - "Name": athlete display name (used in the heading)
func Readme(vars map[string]string) ([]byte, error) {
	raw, err := content.ReadFile("README.md.tmpl")
	if err != nil {
		return nil, err
	}
	t, err := template.New("readme").Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if err := t.Execute(&b, vars); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// SkillFile reads a single embedded skill file. name is the skill
// directory under skills/ (e.g. "workout-builder"); rel is the path
// inside that directory (e.g. "SKILL.md").
func SkillFile(name, rel string) ([]byte, error) {
	return content.ReadFile(path.Join("skills", name, rel))
}

// SkillNames returns the embedded skill directory names in stable
// order.
func SkillNames() ([]string, error) {
	entries, err := content.ReadDir("skills")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// WalkSkill calls fn for every embedded file under skills/<name>/.
// Path arguments to fn are relative to the skill root.
func WalkSkill(name string, fn func(rel string, data []byte) error) error {
	root := path.Join("skills", name)
	return fs.WalkDir(content, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := content.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		rel, err := relPath(root, p)
		if err != nil {
			return err
		}
		return fn(rel, data)
	})
}

func relPath(root, full string) (string, error) {
	if !strings.HasPrefix(full, root+"/") && full != root {
		return "", fmt.Errorf("path %q not under %q", full, root)
	}
	if full == root {
		return ".", nil
	}
	return full[len(root)+1:], nil
}

// Gitignore is the template `.gitignore` written into the workspace.
// We exclude .cache/ by default (it can grow and is regenerable from
// intervals.icu) but track agent-owned narrative files.
const Gitignore = `# fit-agent: cached intervals.icu payloads can be regenerated.
fit-agent/.cache/
`
