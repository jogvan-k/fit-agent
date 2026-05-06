// Package cli — `fit-agent init` command.
//
// init scaffolds a new workspace and stores per-user configuration. It
// is the first command a user runs.
//
// Behavior:
//
//   - Without any flags, prompts interactively (charmbracelet/huh) for
//     the workspace directory, profile name, and intervals.icu API key.
//   - With --non-interactive, takes everything from flags / env and
//     never prompts. --api-key is required in this mode (FIT_AGENT_API_KEY
//     env var is also honored).
//   - Validates the API key by calling /athlete/0; the resolved athlete
//     id and timezone are cached in the profile.
//   - Idempotent: agent-owned files that already exist are kept as-is
//     unless --force is set. .cache/ and machine-owned dirs are always
//     ensured.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/jogvan-k/fit-agent/internal/config"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/templates"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// EnvAPIKey is the environment variable inspected as a fallback when
// --api-key is not supplied and we are running non-interactively.
const EnvAPIKey = "FIT_AGENT_API_KEY"

// initOptions are the resolved arguments after flag parsing and (in
// interactive mode) prompts.
type initOptions struct {
	WorkspaceDir   string
	APIKey         string
	ProfileName    string
	NonInteractive bool
	Force          bool
	SkipValidation bool

	// Filled in by validateAPIKey.
	AthleteID   string
	AthleteName string
	Timezone    string
}

func newInitCmd() *cobra.Command {
	opts := &initOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a workspace and configure intervals.icu credentials",
		Long: `Initialize a fit-agent workspace.

Writes (only files marked * are created if missing; existing files are
kept as-is unless --force is set):

  <workspace>/ATHLETE-PROFILE.md     *  agent-owned, edit freely
  <workspace>/README.md              *  agent-owned overview
  <workspace>/skills/<name>/SKILL.md *  coaching skill prompts
  <workspace>/.fit-agent.toml        *  profile pointer, no secrets
  <workspace>/.gitignore             *  excludes fit-agent/.cache/
  <workspace>/fit-agent/...             machine-owned data + cache dirs
  $XDG_CONFIG_HOME/fit-agent/config.toml mode 0600 (key in OS keyring)

Run interactively for the first-time setup, or pass all values via
flags + --non-interactive for scripting.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.WorkspaceDir, "workspace", "", "workspace directory (default: $PWD)")
	cmd.Flags().StringVar(&opts.APIKey, "api-key", "", "intervals.icu API key (or "+EnvAPIKey+")")
	cmd.Flags().StringVar(&opts.ProfileName, "profile-name", config.DefaultProfile, "configuration profile name")
	cmd.Flags().BoolVar(&opts.NonInteractive, "non-interactive", false, "take all values from flags; never prompt")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "overwrite agent-owned files that already exist")
	cmd.Flags().BoolVar(&opts.SkipValidation, "skip-validation", false, "do not call intervals.icu to validate the API key")
	return cmd
}

func runInit(cmd *cobra.Command, opts *initOptions) error {
	out := cmd.OutOrStdout()
	dryRun, _ := cmd.Root().PersistentFlags().GetBool("dry-run")

	if err := opts.applyEnv(); err != nil {
		return err
	}
	if err := opts.resolveDefaults(); err != nil {
		return err
	}

	if opts.NonInteractive {
		if opts.APIKey == "" {
			return fmt.Errorf("--api-key (or %s) is required in --non-interactive mode", EnvAPIKey)
		}
	} else if err := opts.runForm(); err != nil {
		return err
	}

	if opts.APIKey == "" {
		return errors.New("API key is required")
	}
	if opts.ProfileName == "" {
		opts.ProfileName = config.DefaultProfile
	}

	// Validate the API key against intervals.icu (and detect athlete id
	// + timezone) unless explicitly skipped.
	if !opts.SkipValidation {
		if err := opts.validateAPIKey(cmd.Context()); err != nil {
			return fmt.Errorf("validate intervals.icu API key: %w", err)
		}
		fmt.Fprintf(out, "validated intervals.icu credentials for athlete %s (%s, tz=%s)\n",
			opts.AthleteID, opts.AthleteName, opts.Timezone)
	} else {
		fmt.Fprintln(out, "skipping API key validation (--skip-validation)")
	}

	// Scaffold the workspace.
	layout := workspace.New(opts.WorkspaceDir)
	plan, err := buildScaffoldPlan(layout, opts)
	if err != nil {
		return err
	}
	if err := executeScaffold(out, layout, plan, opts.Force, dryRun); err != nil {
		return err
	}

	// Persist the XDG config + secret.
	if err := writeConfigEntry(out, opts, dryRun); err != nil {
		return err
	}

	if !dryRun {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "next: run `fit-agent fetch --since 30d` to pull recent data.")
	}
	return nil
}

// applyEnv pulls in env-var fallbacks for values not set on the command line.
func (opts *initOptions) applyEnv() error {
	if opts.APIKey == "" {
		opts.APIKey = strings.TrimSpace(os.Getenv(EnvAPIKey))
	}
	return nil
}

// resolveDefaults fills in WorkspaceDir from $PWD when empty and
// canonicalizes the value to an absolute path.
func (opts *initOptions) resolveDefaults() error {
	if opts.WorkspaceDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}
		opts.WorkspaceDir = wd
	}
	abs, err := filepath.Abs(opts.WorkspaceDir)
	if err != nil {
		return fmt.Errorf("abs(%s): %w", opts.WorkspaceDir, err)
	}
	opts.WorkspaceDir = abs
	if opts.ProfileName == "" {
		opts.ProfileName = config.DefaultProfile
	}
	return nil
}

// runForm prompts for any unset values via huh. It only prompts for
// fields that are not already populated, so re-running with --workspace
// just asks for the API key.
func (opts *initOptions) runForm() error {
	groups := []*huh.Group{}

	if opts.WorkspaceDir == "" {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Workspace directory").
				Description("Where should the agent-facing files live?").
				Value(&opts.WorkspaceDir).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("required")
					}
					return nil
				}),
		))
	}
	if opts.ProfileName == "" || opts.ProfileName == config.DefaultProfile {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Profile name").
				Description("Used in $XDG_CONFIG_HOME/fit-agent/config.toml; 'default' is fine.").
				Value(&opts.ProfileName),
		))
	}
	if opts.APIKey == "" {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("intervals.icu API key").
				Description("Find it at intervals.icu → Settings → API. Stored in your OS keyring.").
				EchoMode(huh.EchoModePassword).
				Value(&opts.APIKey).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("required")
					}
					return nil
				}),
		))
	}
	if len(groups) == 0 {
		return nil
	}
	form := huh.NewForm(groups...)
	if err := form.Run(); err != nil {
		return fmt.Errorf("interactive prompt: %w", err)
	}
	// Re-canonicalize workspace dir in case the user typed a relative path.
	return opts.resolveDefaults()
}

// validateAPIKey calls /athlete/0 to confirm the key works and to
// resolve the athlete id ("self" alias) and timezone.
func (opts *initOptions) validateAPIKey(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c, err := icu.NewClient(opts.APIKey, icu.Options{
		BaseURL:    os.Getenv("ICU_BASE_URL"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	})
	if err != nil {
		return err
	}
	a, err := c.GetAthlete(ctx, icu.SelfAthleteID)
	if err != nil {
		return err
	}
	opts.AthleteID = a.ID
	opts.AthleteName = a.Name
	opts.Timezone = a.Timezone
	if opts.AthleteName == "" {
		opts.AthleteName = "Athlete"
	}
	return nil
}

// scaffoldFile is one item in the scaffold plan.
type scaffoldFile struct {
	Path    string
	Content []byte
}

func buildScaffoldPlan(l workspace.Layout, opts *initOptions) ([]scaffoldFile, error) {
	plan := []scaffoldFile{}

	prof, err := templates.AthleteProfile()
	if err != nil {
		return nil, fmt.Errorf("load ATHLETE-PROFILE template: %w", err)
	}
	plan = append(plan, scaffoldFile{Path: l.AthleteProfilePath(), Content: prof})

	readme, err := templates.Readme(map[string]string{
		"Name": firstWord(opts.AthleteName),
	})
	if err != nil {
		return nil, fmt.Errorf("render README template: %w", err)
	}
	plan = append(plan, scaffoldFile{Path: l.ReadmePath(), Content: readme})

	plan = append(plan, scaffoldFile{
		Path:    filepath.Join(l.Root, ".gitignore"),
		Content: []byte(templates.Gitignore),
	})
	plan = append(plan, scaffoldFile{
		Path:    l.PointerPath(),
		Content: []byte(fmt.Sprintf("profile = %q\n", opts.ProfileName)),
	})

	names, err := templates.SkillNames()
	if err != nil {
		return nil, fmt.Errorf("enumerate skills: %w", err)
	}
	for _, name := range names {
		err := templates.WalkSkill(name, func(rel string, data []byte) error {
			plan = append(plan, scaffoldFile{
				Path:    filepath.Join(l.SkillsDir(), name, rel),
				Content: data,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk skill %s: %w", name, err)
		}
	}

	// Every scaffolded file is agent-owned; verify before writing.
	for _, f := range plan {
		if err := l.GuardWrite(f.Path, workspace.OwnerAgent); err != nil {
			return nil, fmt.Errorf("ownership check %s: %w", f.Path, err)
		}
	}
	return plan, nil
}

func executeScaffold(out io.Writer, l workspace.Layout, plan []scaffoldFile, force, dryRun bool) error {
	for _, f := range plan {
		exists := fileExists(f.Path)
		switch {
		case exists && !force:
			fmt.Fprintf(out, "  keep      %s\n", f.Path)
		case dryRun:
			verb := "create   "
			if exists {
				verb = "overwrite"
			}
			fmt.Fprintf(out, "  %s %s (%d bytes)\n", verb, f.Path, len(f.Content))
		default:
			if err := workspace.AtomicWrite(f.Path, f.Content, 0); err != nil {
				return fmt.Errorf("write %s: %w", f.Path, err)
			}
			verb := "wrote    "
			if exists {
				verb = "overwrote"
			}
			fmt.Fprintf(out, "  %s %s\n", verb, f.Path)
		}
	}
	if dryRun {
		for _, d := range l.MachineDirs() {
			fmt.Fprintf(out, "  mkdir     %s\n", d)
		}
		return nil
	}
	if err := l.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure machine dirs: %w", err)
	}
	return nil
}

func writeConfigEntry(out io.Writer, opts *initOptions, dryRun bool) error {
	cfgPath, err := config.Path()
	if err != nil {
		return err
	}

	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		if !errors.Is(err, config.ErrNotFound) {
			return err
		}
		cfg = &config.Config{Profiles: map[string]config.Profile{}}
	}

	prof := config.Profile{
		Workspace:    opts.WorkspaceDir,
		IcuAthleteID: opts.AthleteID,
		IcuTimezone:  opts.Timezone,
	}

	if dryRun {
		fmt.Fprintf(out, "  write     %s (profile %q, athlete %s)\n", cfgPath, opts.ProfileName, opts.AthleteID)
		fmt.Fprintf(out, "  keyring   set fit-agent/%s\n", opts.ProfileName)
		return nil
	}

	store := config.SecretStore(config.KeyringStore{})
	keyringFailed := false
	if err := store.Set(opts.ProfileName, opts.APIKey); err != nil {
		fmt.Fprintf(out, "  warn      OS keyring unavailable; storing API key in %s (mode 0600)\n", cfgPath)
		prof.IcuAPIKey = opts.APIKey
		keyringFailed = true
	}
	cfg.Set(opts.ProfileName, prof)

	if err := cfg.SaveTo(cfgPath); err != nil {
		return err
	}
	if keyringFailed {
		fmt.Fprintf(out, "  wrote     %s (key in file fallback)\n", cfgPath)
	} else {
		fmt.Fprintf(out, "  wrote     %s (key in OS keyring)\n", cfgPath)
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Athlete"
	}
	if i := strings.IndexAny(s, " \t"); i > 0 {
		return s[:i]
	}
	return s
}
