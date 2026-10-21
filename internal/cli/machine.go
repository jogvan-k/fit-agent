package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/plannedio"
	"github.com/jogvan-k/fit-agent/internal/syncorch"
	"github.com/spf13/cobra"
)

type layerError struct {
	layer string
	err   error
}

func (e *layerError) Error() string { return e.err.Error() }
func (e *layerError) Unwrap() error { return e.err }

// machineResult emits only an allowlisted error category, never wrapped HTTP
// bodies, athlete content, config values or secret-store error text.
func machineResult(cmd *cobra.Command, data any, err error) error {
	if err == nil {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"schemaVersion": 1, "ok": true, "data": data})
	}
	layer := "validation"
	var le *layerError
	var ne net.Error
	var pe *os.PathError
	switch {
	case errors.As(err, &le):
		layer = le.layer
	case errors.Is(err, icu.ErrUnauthorized), errors.Is(err, icu.ErrForbidden):
		layer = "Intervals authentication"
	case errors.Is(err, syncorch.ErrConflict), errors.Is(err, syncorch.ErrStalePreview):
		layer = "sync conflict"
	case errors.As(err, &ne):
		layer = "network"
	case errors.Is(err, icu.ErrRateLimited), errors.Is(err, icu.ErrServer):
		layer = "network"
	case errors.As(err, &pe):
		layer = "workspace"
	}
	message := "Command failed; inspect the local CLI in a private terminal."
	if errors.Is(err, syncorch.ErrStalePreview) {
		message = "Preview changed; preview again and confirm the matching result."
	}
	if errors.Is(err, syncorch.ErrConflict) {
		message = "Reconcile local and Intervals-authored changes manually, then preview again."
	}
	if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"schemaVersion": 1, "ok": false,
		"error": map[string]string{"layer": layer, "message": message}}); encodeErr != nil {
		return encodeErr
	}
	return fmt.Errorf("%s: %s", layer, message)
}

func newIntegrationInfoCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "integration-info", Short: "Report versioned machine integration capabilities", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return machineResult(cmd, map[string]any{"cliVersion": Version, "protocol": 1,
				"features": []string{"doctor-json", "status-json", "fetch-json", "guarded-sync-v1", "workspace-assertion"}}, nil)
		}}
	cmd.Flags().Bool("json", false, "machine-readable output (always enabled for this command)")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "Read-only local profile and credential readiness (no network)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, _, err := resolveRuntime(cmd)
			if err != nil {
				return machineResult(cmd, nil, err)
			}
			if r.Profile.IcuAthleteID == "" || r.Profile.IcuTimezone == "" {
				return machineResult(cmd, nil, &layerError{layer: "profile/config", err: errors.New("athlete or timezone missing")})
			}
			return machineResult(cmd, map[string]any{"workspace": r.Layout.Root, "profile": r.ProfileName,
				"timezone": r.Location.String(), "credentialsReady": true}, nil)
		}}
	cmd.Flags().Bool("json", false, "machine-readable output (always enabled for this command)")
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Content-free workspace health summary (no network)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, _, err := resolveRuntime(cmd)
			if err != nil {
				return machineResult(cmd, nil, err)
			}
			var activity, wellness, modified *string
			var latest time.Time
			for _, dir := range []string{r.Layout.ActivitiesDir(), r.Layout.WellnessDir()} {
				entries, err := os.ReadDir(dir)
				if err != nil {
					return machineResult(cmd, nil, err)
				}
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
						continue
					}
					value := strings.TrimSuffix(e.Name(), ".yaml")
					format, target := "2006-01-02", &activity
					if dir == r.Layout.WellnessDir() {
						format, target = "2006-01", &wellness
					}
					if _, err := time.Parse(format, value); err != nil {
						continue
					}
					if *target == nil || value > **target {
						v := value
						*target = &v
					}
					info, err := e.Info()
					if err != nil {
						return machineResult(cmd, nil, err)
					}
					if info.ModTime().After(latest) {
						latest = info.ModTime()
					}
				}
			}
			if !latest.IsZero() {
				v := latest.UTC().Format(time.RFC3339)
				modified = &v
			}
			entries, err := os.ReadDir(r.Layout.PlannedWorkoutsDir())
			if err != nil {
				return machineResult(cmd, nil, err)
			}
			count := 0
			dates := map[string]bool{}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				day, err := plannedio.ReadDay(filepath.Join(r.Layout.PlannedWorkoutsDir(), e.Name()))
				if err != nil {
					return machineResult(cmd, nil, err)
				}
				count += len(day.Workouts)
				if day.Date >= time.Now().In(r.Location).Format("2006-01-02") && len(day.Workouts) > 0 {
					dates[day.Date] = true
				}
			}
			upcoming := make([]string, 0, len(dates))
			for d := range dates {
				upcoming = append(upcoming, d)
			}
			sort.Strings(upcoming)
			if len(upcoming) > 30 {
				upcoming = upcoming[:30]
			}
			return machineResult(cmd, map[string]any{"workspace": r.Layout.Root, "latestActivityDate": activity,
				"latestWellnessMonth": wellness, "dataModifiedAt": modified, "plannedWorkoutCount": count,
				"upcomingDates": upcoming, "athleteProfileExists": fileExists(r.Layout.AthleteProfilePath()),
				"trainingPlanExists": fileExists(r.Layout.TrainingPlanPath())}, nil)
		}}
	cmd.Flags().Bool("json", false, "machine-readable output (always enabled for this command)")
	return cmd
}
