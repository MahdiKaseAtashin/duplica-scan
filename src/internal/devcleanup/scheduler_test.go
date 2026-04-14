package devcleanup

import (
	"path/filepath"
	"testing"
	"time"
)

func TestShouldRunScheduleWeekly(t *testing.T) {
	now := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	state := ScheduleState{LastRunAt: now.AddDate(0, 0, -8)}
	if !ShouldRunSchedule(now, ScheduleWeekly, state) {
		t.Fatalf("expected weekly schedule to run after a week boundary")
	}
}

func TestShouldRunScheduleMonthly(t *testing.T) {
	now := time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)
	state := ScheduleState{LastRunAt: time.Date(2026, time.March, 31, 10, 0, 0, 0, time.UTC)}
	if !ShouldRunSchedule(now, ScheduleMonthly, state) {
		t.Fatalf("expected monthly schedule to run in a new month")
	}
}

func TestSaveAndLoadScheduleState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	in := ScheduleState{LastRunAt: time.Date(2026, time.April, 14, 10, 0, 0, 0, time.UTC)}
	if err := saveScheduleState(path, in); err != nil {
		t.Fatalf("save state: %v", err)
	}
	out, err := loadScheduleState(path)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if !out.LastRunAt.Equal(in.LastRunAt) {
		t.Fatalf("unexpected state time: got=%v want=%v", out.LastRunAt, in.LastRunAt)
	}
}

func TestLoadScheduleStateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	state, err := loadScheduleState(path)
	if err != nil {
		t.Fatalf("load missing state: %v", err)
	}
	if !state.LastRunAt.IsZero() {
		t.Fatalf("expected zero state for missing file")
	}
}

func TestBuiltinSafeProfileDefaults(t *testing.T) {
	profile := BuiltinSafeProfile("quick-safe")
	if profile.Config.MaxRisk != RiskSafe {
		t.Fatalf("expected safe profile risk level")
	}
	if profile.Config.DryRun {
		t.Fatalf("expected dry-run false")
	}
	if !profile.Config.ProcessAware {
		t.Fatalf("expected process-aware true")
	}
	if !profile.Config.DisableCommandTasks {
		t.Fatalf("expected command tasks disabled")
	}
}

