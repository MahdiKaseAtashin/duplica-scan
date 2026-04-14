package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"cleanpulse/src/internal/devcleanup"
)

func main() {
	maxRisk := flag.String("risk", "safe", "Maximum risk level: safe|moderate|aggressive")
	dryRun := flag.Bool("dry-run", false, "Dry run mode (plan only)")
	yes := flag.Bool("yes", false, "Skip interactive confirmations")
	verbose := flag.Bool("verbose", false, "Verbose logging")
	parallelism := flag.Int("parallelism", runtime.NumCPU(), "Parallel workers for size planning")
	minAgeHours := flag.Int("min-age-hours", 24, "Delete only entries older than this age")
	deleteMode := flag.String("delete-mode", "delete", "Deletion mode: delete|quarantine")
	quarantineDir := flag.String("quarantine-dir", "", "Optional quarantine directory when delete-mode=quarantine")
	processAware := flag.Bool("process-aware", true, "Skip IDE/browser cache tasks when related apps are running")
	includeCategories := flag.String("include-categories", "", "Comma-separated categories to include")
	includeIDs := flag.String("include-ids", "", "Comma-separated cleanup IDs to include")
	excludeIDs := flag.String("exclude-ids", "", "Comma-separated cleanup IDs to exclude")
	patternRoots := flag.String("pattern-roots", "", "Pattern roots as task=path1|path2,task2=path3")
	configPath := flag.String("config", "", "Optional JSON config file")
	reportPath := flag.String("report", "", "Optional output report path")
	reportFormat := flag.String("report-format", "json", "Report format: json|md|html")
	schedule := flag.String("schedule", "off", "Schedule mode: off|weekly|monthly")
	scheduleState := flag.String("schedule-state", "", "Path to scheduler state file")
	scheduledProfile := flag.String("scheduled-profile", "quick-safe", "Scheduled built-in profile name")
	scheduledReportDir := flag.String("scheduled-report-dir", "", "Directory for auto-exported scheduled reports")
	flag.Parse()

	cfg := devcleanup.Config{
		MaxRisk:           devcleanup.ParseRiskLevel(strings.TrimSpace(strings.ToLower(*maxRisk))),
		DryRun:            *dryRun,
		AssumeYes:         *yes,
		Verbose:           *verbose,
		DeleteMode:        strings.TrimSpace(strings.ToLower(*deleteMode)),
		QuarantineDir:     strings.TrimSpace(*quarantineDir),
		Parallelism:       *parallelism,
		MinAge:            time.Duration(*minAgeHours) * time.Hour,
		ProcessAware:      *processAware,
		IncludeCategories: csvSet(*includeCategories),
		IncludeIDs:        csvSet(*includeIDs),
		ExcludeIDs:        csvSet(*excludeIDs),
		PathOverrides:     map[string][]string{},
		PatternRoots:      parsePatternRoots(*patternRoots),
	}

	if *configPath != "" {
		fileCfg, err := devcleanup.LoadFileConfig(*configPath)
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		mergeConfig(&cfg, fileCfg)
	}

	env, err := environment()
	if err != nil {
		log.Fatalf("failed to detect environment: %v", err)
	}
	engine := devcleanup.NewEngine(
		devcleanup.BuiltinProviders(env),
		devcleanup.Logger{Out: os.Stdout, Verbose: cfg.Verbose},
		devcleanup.NewConsolePrompt(os.Stdin, os.Stdout),
	)
	scheduleMode := strings.TrimSpace(strings.ToLower(*schedule))
	if scheduleMode != "" && scheduleMode != "off" {
		statePath := strings.TrimSpace(*scheduleState)
		if statePath == "" {
			statePath = ".cleanpulse/scheduler-state.json"
		}
		reportDir := strings.TrimSpace(*scheduledReportDir)
		if reportDir == "" {
			reportDir = ".cleanpulse/reports"
		}
		kind := devcleanup.ScheduleKind(scheduleMode)
		if kind != devcleanup.ScheduleWeekly && kind != devcleanup.ScheduleMonthly {
			log.Fatalf("invalid schedule: %s (allowed: off|weekly|monthly)", scheduleMode)
		}
		profile := devcleanup.BuiltinSafeProfile(strings.TrimSpace(strings.ToLower(*scheduledProfile)))
		report, ran, err := devcleanup.RunScheduledCleanup(context.Background(), engine, profile, kind, statePath, reportDir)
		if err != nil {
			log.Fatalf("scheduled cleanup failed: %v", err)
		}
		if !ran {
			fmt.Printf("Scheduled cleanup skipped (%s): already executed in current window\n", scheduleMode)
			return
		}
		devcleanup.PrintRunSummary(os.Stdout, report)
		fmt.Printf("Scheduled run completed. Reports saved in %s\n", reportDir)
		return
	}

	report, err := engine.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("cleanup run failed: %v", err)
	}
	devcleanup.PrintRunSummary(os.Stdout, report)
	if *reportPath != "" {
		if err := writeReport(*reportPath, *reportFormat, report); err != nil {
			log.Fatalf("failed to write report: %v", err)
		}
		fmt.Printf("Report written to %s\n", *reportPath)
	}
}

func mergeConfig(cfg *devcleanup.Config, fileCfg devcleanup.FileConfig) {
	if fileCfg.MaxRisk != "" {
		cfg.MaxRisk = devcleanup.ParseRiskLevel(strings.TrimSpace(strings.ToLower(fileCfg.MaxRisk)))
	}
	if fileCfg.Parallelism > 0 {
		cfg.Parallelism = fileCfg.Parallelism
	}
	if fileCfg.MinAgeHours > 0 {
		cfg.MinAge = time.Duration(fileCfg.MinAgeHours) * time.Hour
	}
	if len(fileCfg.IncludeCategories) > 0 {
		cfg.IncludeCategories = csvSliceSet(fileCfg.IncludeCategories)
	}
	if len(fileCfg.IncludeIDs) > 0 {
		cfg.IncludeIDs = csvSliceSet(fileCfg.IncludeIDs)
	}
	if len(fileCfg.ExcludeIDs) > 0 {
		cfg.ExcludeIDs = csvSliceSet(fileCfg.ExcludeIDs)
	}
	if len(fileCfg.PathOverrides) > 0 {
		cfg.PathOverrides = fileCfg.PathOverrides
	}
	if fileCfg.ProcessAware != nil {
		cfg.ProcessAware = *fileCfg.ProcessAware
	}
	if len(fileCfg.PatternRoots) > 0 {
		cfg.PatternRoots = fileCfg.PatternRoots
	}
}

func environment() (devcleanup.Environment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return devcleanup.Environment{}, err
	}
	return devcleanup.Environment{
		OS:      runtime.GOOS,
		HomeDir: home,
		TempDir: os.TempDir(),
	}, nil
}

func csvSet(raw string) map[string]struct{} {
	if strings.TrimSpace(raw) == "" {
		return map[string]struct{}{}
	}
	return csvSliceSet(strings.Split(raw, ","))
}

func csvSliceSet(values []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range values {
		clean := strings.TrimSpace(strings.ToLower(value))
		if clean == "" {
			continue
		}
		out[clean] = struct{}{}
	}
	return out
}

func parsePatternRoots(raw string) map[string][]string {
	result := make(map[string][]string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.SplitN(part, "=", 2)
		if len(segments) != 2 {
			continue
		}
		id := strings.TrimSpace(strings.ToLower(segments[0]))
		if id == "" {
			continue
		}
		paths := make([]string, 0, 4)
		for _, path := range strings.Split(segments[1], "|") {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			paths = append(paths, path)
		}
		if len(paths) == 0 {
			continue
		}
		result[id] = paths
	}
	return result
}

func writeReport(path, format string, report devcleanup.RunReport) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return devcleanup.WriteJSONReport(path, report)
	case "md", "markdown":
		return devcleanup.WriteMarkdownReport(path, report)
	case "html":
		return devcleanup.WriteHTMLReport(path, report)
	default:
		return fmt.Errorf("unsupported report format: %s", format)
	}
}
