package devcleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ScheduleKind string

const (
	ScheduleWeekly  ScheduleKind = "weekly"
	ScheduleMonthly ScheduleKind = "monthly"
)

type ScheduledProfile struct {
	Name string `json:"name"`
	Config
}

type ScheduleState struct {
	LastRunAt time.Time `json:"last_run_at"`
}

func BuiltinSafeProfile(name string) ScheduledProfile {
	return ScheduledProfile{
		Name: name,
		Config: Config{
			MaxRisk:             RiskSafe,
			DryRun:              false,
			AssumeYes:           true,
			Verbose:             false,
			DisableCommandTasks: true,
			DeleteMode:          "delete",
			Parallelism:         4,
			MinAge:              24 * time.Hour,
			ProcessAware:        true,
			IncludeCategories: map[string]struct{}{
				"os-temp":         {},
				"package-manager": {},
				"logs":            {},
				"browser":         {},
			},
			ExcludeIDs:    map[string]struct{}{},
			IncludeIDs:    map[string]struct{}{},
			PathOverrides: map[string][]string{},
			PatternRoots:  map[string][]string{},
		},
	}
}

func ShouldRunSchedule(now time.Time, kind ScheduleKind, state ScheduleState) bool {
	if state.LastRunAt.IsZero() {
		return true
	}
	switch kind {
	case ScheduleMonthly:
		y1, m1, _ := state.LastRunAt.Date()
		y2, m2, _ := now.Date()
		return y1 != y2 || m1 != m2
	case ScheduleWeekly:
		fallthrough
	default:
		lastYear, lastWeek := state.LastRunAt.ISOWeek()
		nowYear, nowWeek := now.ISOWeek()
		return lastYear != nowYear || lastWeek != nowWeek
	}
}

func RunScheduledCleanup(
	ctx context.Context,
	engine *Engine,
	profile ScheduledProfile,
	schedule ScheduleKind,
	statePath string,
	reportDir string,
) (RunReport, bool, error) {
	state, err := loadScheduleState(statePath)
	if err != nil {
		return RunReport{}, false, err
	}
	now := time.Now()
	if !ShouldRunSchedule(now, schedule, state) {
		return RunReport{}, false, nil
	}

	report, err := engine.Run(ctx, profile.Config)
	if err != nil {
		return RunReport{}, false, err
	}
	reportPath := filepath.Join(reportDir, fmt.Sprintf("scheduled-cleanup-%s-%s.json", profile.Name, now.Format("20060102-150405")))
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return RunReport{}, false, err
	}
	if err := WriteJSONReport(reportPath, report); err != nil {
		return RunReport{}, false, err
	}

	state.LastRunAt = now
	if err := saveScheduleState(statePath, state); err != nil {
		return RunReport{}, false, err
	}
	return report, true, nil
}

func loadScheduleState(path string) (ScheduleState, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ScheduleState{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ScheduleState{}, nil
		}
		return ScheduleState{}, err
	}
	var state ScheduleState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ScheduleState{}, err
	}
	return state, nil
}

func saveScheduleState(path string, state ScheduleState) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

