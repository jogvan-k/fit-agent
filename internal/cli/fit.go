package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/jogvan-k/fit-agent/internal/fitparse"
	"github.com/spf13/cobra"
)

// newFitCmd returns the `fit-agent fit` subtree: pure FIT inspection,
// no network, no workspace I/O.
func newFitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fit",
		Short: "Inspect a parsed .fit file (no network, no workspace I/O)",
		Long: `fit groups subcommands that decode and pretty-print FIT
files. Useful for confirming that internal/fitparse extracts the
expected lap and interval data before worrying about render output.`,
	}
	cmd.AddCommand(newFitSummaryCmd(), newFitLapsCmd(), newFitDumpCmd())
	return cmd
}

func newFitSummaryCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "summary <path-to.fit>",
		Short: "Print session-level metrics for a .fit file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := fitparse.Decode(args[0])
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON {
				return writeJSON(w, newSummaryView(a))
			}
			fmt.Fprintf(w, "sport:        %s\n", blank(a.Sport))
			fmt.Fprintf(w, "start:        %s\n", formatTime(a.StartLocal))
			fmt.Fprintf(w, "elapsed:      %s\n", formatDuration(a.TotalTime))
			fmt.Fprintf(w, "moving:       %s\n", formatDuration(a.MovingTime))
			fmt.Fprintf(w, "distance:     %.1f m\n", a.Distance)
			fmt.Fprintf(w, "avg hr:       %d bpm\n", a.AvgHR)
			fmt.Fprintf(w, "max hr:       %d bpm\n", a.MaxHR)
			fmt.Fprintf(w, "avg power:    %d W\n", a.AvgPower)
			fmt.Fprintf(w, "avg cadence:  %d rpm\n", a.AvgCadence)
			fmt.Fprintf(w, "avg speed:    %.3f m/s\n", a.AvgSpeed)
			fmt.Fprintf(w, "calories:     %d kcal\n", a.Calories)
			fmt.Fprintf(w, "laps:         %d\n", len(a.Laps))
			fmt.Fprintf(w, "intervals:    %d\n", len(a.Intervals))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func newFitLapsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "laps <path-to.fit>",
		Short: "Print one row per lap (table or --json)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := fitparse.Decode(args[0])
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON {
				return writeJSON(w, a.Laps)
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "#\tintensity\ttrigger\tstep\tduration\tdistance\thr\tmaxhr\tpwr\tcad\tpace")
			for _, l := range a.Laps {
				fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%s\t%.0f\t%d\t%d\t%d\t%d\t%s\n",
					l.Index,
					blank(l.Intensity),
					blank(l.Trigger),
					l.WorkoutStepIndex,
					formatDuration(l.Duration),
					l.Distance,
					l.AvgHR,
					l.MaxHR,
					l.AvgPower,
					l.AvgCadence,
					formatPace(l.AvgPaceSecPerKm),
				)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func newFitDumpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump <path-to.fit>",
		Short: "Dump the parsed activity (laps + intervals) as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := fitparse.Decode(args[0])
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), a)
		},
	}
	return cmd
}

// summaryView is a JSON-friendly projection of the session-level fields.
// Exposed as its own type so the JSON output is stable even if
// [fitparse.ParsedActivity] grows more fields later.
type summaryView struct {
	Sport       string  `json:"sport"`
	StartLocal  string  `json:"start_local"`
	TotalTimeS  float64 `json:"total_time_s"`
	MovingTimeS float64 `json:"moving_time_s"`
	DistanceM   float64 `json:"distance_m"`
	AvgHR       int     `json:"avg_hr"`
	MaxHR       int     `json:"max_hr"`
	AvgPower    int     `json:"avg_power"`
	AvgCadence  int     `json:"avg_cadence"`
	AvgSpeedMS  float64 `json:"avg_speed_m_s"`
	Calories    int     `json:"calories"`
	Laps        int     `json:"laps"`
	Intervals   int     `json:"intervals"`
}

func newSummaryView(a *fitparse.ParsedActivity) summaryView {
	return summaryView{
		Sport:       a.Sport,
		StartLocal:  formatTime(a.StartLocal),
		TotalTimeS:  a.TotalTime.Seconds(),
		MovingTimeS: a.MovingTime.Seconds(),
		DistanceM:   a.Distance,
		AvgHR:       a.AvgHR,
		MaxHR:       a.MaxHR,
		AvgPower:    a.AvgPower,
		AvgCadence:  a.AvgCadence,
		AvgSpeedMS:  a.AvgSpeed,
		Calories:    a.Calories,
		Laps:        len(a.Laps),
		Intervals:   len(a.Intervals),
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02T15:04:05Z07:00")
}

func formatPace(secPerKm int) string {
	if secPerKm <= 0 {
		return "-"
	}
	m := secPerKm / 60
	s := secPerKm % 60
	return fmt.Sprintf("%d:%02d/km", m, s)
}

func blank(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
