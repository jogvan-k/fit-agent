package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// WorkspaceFile is the per-workspace pointer file. It contains no
// secrets; it identifies which configuration profile applies to the
// workspace.
const WorkspaceFile = ".fit-agent.toml"

// WorkspacePointer is the on-disk schema for the workspace pointer file.
type WorkspacePointer struct {
	// Profile is the configuration profile name to use within this
	// workspace. Falls back to [DefaultProfile] when empty.
	Profile string `toml:"profile"`
}

// LoadWorkspacePointer reads ".fit-agent.toml" from the given workspace
// directory. Returns an empty pointer (no error) if the file is missing,
// since a workspace without a pointer simply uses the default profile.
func LoadWorkspacePointer(workspace string) (WorkspacePointer, error) {
	path := filepath.Join(workspace, WorkspaceFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WorkspacePointer{}, nil
		}
		return WorkspacePointer{}, fmt.Errorf("read %s: %w", path, err)
	}
	var p WorkspacePointer
	if _, err := toml.Decode(string(data), &p); err != nil {
		return WorkspacePointer{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return p, nil
}

// SaveWorkspacePointer writes ".fit-agent.toml" into the given workspace
// directory atomically with mode 0644.
func SaveWorkspacePointer(workspace string, p WorkspacePointer) error {
	path := filepath.Join(workspace, WorkspaceFile)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	tmp, err := os.CreateTemp(workspace, ".fit-agent-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := toml.NewEncoder(tmp).Encode(p); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode pointer: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// ResolveProfile returns the active profile name using this precedence:
//  1. flag value (if non-empty),
//  2. FIT_AGENT_PROFILE env var,
//  3. workspace ".fit-agent.toml" Profile field,
//  4. [DefaultProfile].
//
// workspace may be the empty string when no workspace is in scope.
func ResolveProfile(flag, workspace string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv(EnvProfile); env != "" {
		return env, nil
	}
	if workspace != "" {
		p, err := LoadWorkspacePointer(workspace)
		if err != nil {
			return "", err
		}
		if p.Profile != "" {
			return p.Profile, nil
		}
	}
	return DefaultProfile, nil
}
