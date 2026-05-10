package workspace

import (
	"errors"
	"path/filepath"
	"strings"
)

// Owner identifies who is allowed to write a workspace file.
type Owner int

const (
	// OwnerUnknown is the zero value for paths outside the workspace
	// or paths the ownership table does not cover.
	OwnerUnknown Owner = iota
	// OwnerAgent files are written by the agent (or by `init` from a
	// template). `fetch` and `push-workouts` must never overwrite them.
	OwnerAgent
	// OwnerMachine files are regenerated from cache on every fetch.
	// Hand edits are expected to be lost.
	OwnerMachine
	// OwnerShared files are written by both: `fetch` regenerates
	// content and `push-workouts` writes back ids.
	OwnerShared
)

// String returns a stable lowercase label.
func (o Owner) String() string {
	switch o {
	case OwnerAgent:
		return "agent"
	case OwnerMachine:
		return "machine"
	case OwnerShared:
		return "shared"
	default:
		return "unknown"
	}
}

// ErrOwnership is returned by [Layout.GuardWrite] when the caller's
// claimed Owner does not match the path's ownership.
var ErrOwnership = errors.New("workspace ownership violation")

// Classify returns the [Owner] for a path inside the workspace.
//
// Path may be absolute or relative; relative paths are interpreted
// against [Layout.Root]. Paths outside the workspace return
// [OwnerUnknown].
func (l Layout) Classify(path string) Owner {
	rel, ok := l.relPath(path)
	if !ok {
		return OwnerUnknown
	}
	return classifyRel(rel)
}

// GuardWrite returns nil when the given owner is permitted to write
// path under this workspace. Machine writers must use [OwnerMachine];
// `init` and skill scaffolders use [OwnerAgent]; the planned-workout
// writer uses [OwnerShared].
//
// Paths outside the workspace are rejected with [ErrOwnership].
func (l Layout) GuardWrite(path string, owner Owner) error {
	got := l.Classify(path)
	if got == OwnerUnknown {
		return ownershipError(path, owner, got)
	}
	if got == OwnerShared {
		return nil // both writers allowed
	}
	if got != owner {
		return ownershipError(path, owner, got)
	}
	return nil
}

func ownershipError(path string, want, got Owner) error {
	return &OwnershipError{Path: path, Want: want, Got: got}
}

// OwnershipError is the concrete type returned by [Layout.GuardWrite].
type OwnershipError struct {
	Path string
	Want Owner
	Got  Owner
}

// Error implements [error]. Message is stable for tests and matches
// [errors.Is](err, [ErrOwnership]).
func (e *OwnershipError) Error() string {
	return "ownership violation: " + e.Path +
		" is owned by " + e.Got.String() +
		", caller claimed " + e.Want.String()
}

// Unwrap returns [ErrOwnership] so callers can use [errors.Is].
func (e *OwnershipError) Unwrap() error { return ErrOwnership }

// relPath returns path relative to the workspace root, slash-separated,
// and a boolean indicating whether it is inside the workspace.
func (l Layout) relPath(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	root, err := filepath.Abs(l.Root)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// classifyRel returns the owner for a workspace-relative slash path.
//
// Rules (longest-prefix first):
//   - fit-agent/.cache/**                         machine
//   - fit-agent/planned-workouts/**               shared
//   - fit-agent/activities/**                     machine
//   - fit-agent/wellness/**                       machine
//   - skills/**                                   agent
//   - ATHLETE-PROFILE.md                          agent
//   - TRAINING-PLAN.md                            agent
//   - README.md                                   agent
//   - .fit-agent.toml                             agent (init only; not touched by fetch)
//   - everything else                             unknown
func classifyRel(rel string) Owner {
	switch {
	case strings.HasPrefix(rel, "fit-agent/.cache/") || rel == "fit-agent/.cache":
		return OwnerMachine
	case strings.HasPrefix(rel, "fit-agent/planned-workouts/") || rel == "fit-agent/planned-workouts":
		return OwnerShared
	case strings.HasPrefix(rel, "fit-agent/activities/") || rel == "fit-agent/activities":
		return OwnerMachine
	case strings.HasPrefix(rel, "fit-agent/wellness/") || rel == "fit-agent/wellness":
		return OwnerMachine
	case strings.HasPrefix(rel, "skills/"):
		return OwnerAgent
	case rel == "ATHLETE-PROFILE.md",
		rel == "TRAINING-PLAN.md",
		rel == "README.md",
		rel == ".fit-agent.toml",
		rel == ".gitignore":
		return OwnerAgent
	default:
		return OwnerUnknown
	}
}
