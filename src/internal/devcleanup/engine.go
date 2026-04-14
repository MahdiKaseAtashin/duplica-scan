package devcleanup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"cleanpulse/src/internal/cleanup"
)

func (e *Engine) Run(ctx context.Context, cfg Config) (RunReport, error) {
	start := time.Now()
	env, err := collectEnvironment()
	if err != nil {
		return RunReport{}, err
	}
	if cfg.Parallelism < 1 {
		cfg.Parallelism = 1
	}
	if cfg.MaxRisk == 0 {
		cfg.MaxRisk = RiskSafe
	}
	tasks := e.collectTasks(env, cfg)
	plan := e.plan(ctx, tasks, cfg, runningProcesses())
	results := e.execute(ctx, plan, cfg)

	report := RunReport{
		GeneratedAt:       time.Now(),
		OS:                env.OS,
		DryRun:            cfg.DryRun,
		MaxRisk:           cfg.MaxRisk.String(),
		Parallelism:       cfg.Parallelism,
		Planned:           len(plan),
		PlannedByCategory: map[string]int64{},
		FreedByCategory:   map[string]int64{},
		PlannedByVolume:   map[string]int64{},
		FreedByVolume:     map[string]int64{},
		Duration:          time.Since(start),
	}

	for _, item := range plan {
		if item.SkippedReason != "" || item.Err != nil {
			report.Skipped++
		}
		report.PlannedByCategory[item.Task.Category] += item.EstimatedSize
		volumes := volumesForTask(item.Task)
		for volume, portion := range splitBytesAcrossVolumes(item.EstimatedSize, volumes) {
			report.PlannedByVolume[volume] += portion
		}
	}
	estimatedByID := make(map[string]int64, len(plan))
	for _, item := range plan {
		estimatedByID[item.Task.ID] = item.EstimatedSize
	}
	for _, result := range results {
		if result.Attempted {
			report.Attempted++
		}
		// Reclaimed bytes should only include successful actions.
		if result.Attempted && result.Err == nil {
			report.ReclaimedBytes += result.DeletedBytes
			report.FreedByCategory[result.Task.Category] += result.DeletedBytes
			volumes := volumesForTask(result.Task)
			for volume, portion := range splitBytesAcrossVolumes(result.DeletedBytes, volumes) {
				report.FreedByVolume[volume] += portion
			}
		}
		entry := ResultReportEntry{
			ID:             result.Task.ID,
			Name:           result.Task.Name,
			Category:       result.Task.Category,
			Risk:           result.Task.Risk.String(),
			EstimatedBytes: estimatedByID[result.Task.ID],
			Attempted:      result.Attempted,
			DeletedBytes:   result.DeletedBytes,
			DeletedItems:   result.DeletedItems,
			Skipped:        result.Skipped,
		}
		if result.Err != nil {
			entry.Error = result.Err.Error()
		}
		report.Results = append(report.Results, entry)
	}
	return report, nil
}

func splitBytesAcrossVolumes(total int64, volumes []string) map[string]int64 {
	out := make(map[string]int64)
	if total <= 0 {
		return out
	}
	if len(volumes) == 0 {
		out["unknown"] = total
		return out
	}
	base := total / int64(len(volumes))
	remainder := total % int64(len(volumes))
	for i, volume := range volumes {
		share := base
		if int64(i) < remainder {
			share++
		}
		out[volume] += share
	}
	return out
}

func volumesForTask(task CleanupTask) []string {
	set := make(map[string]struct{})
	addVolume := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		volume := filepath.VolumeName(path)
		if volume == "" {
			volume = "/"
		}
		set[volume] = struct{}{}
	}
	if task.PathTask != nil {
		addVolume(task.PathTask.Path)
	}
	if task.PatternTask != nil {
		for _, root := range task.PatternTask.Roots {
			addVolume(root)
		}
	}
	if task.Kind == TaskKindPattern && strings.TrimSpace(task.Description) != "" {
		for _, path := range strings.Split(task.Description, ";") {
			addVolume(path)
		}
	}
	if len(set) == 0 {
		set["unknown"] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for volume := range set {
		out = append(out, volume)
	}
	return out
}

func collectEnvironment() (Environment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Environment{}, err
	}
	return Environment{
		OS:      runtime.GOOS,
		HomeDir: home,
		TempDir: os.TempDir(),
	}, nil
}

func (e *Engine) collectTasks(env Environment, cfg Config) []CleanupTask {
	var tasks []CleanupTask
	for _, provider := range e.providers {
		tasks = append(tasks, provider.Tasks(env)...)
	}

	var filtered []CleanupTask
	for _, task := range tasks {
		if task.Kind == TaskKindCommand && cfg.DisableCommandTasks {
			continue
		}
		if task.Risk > cfg.MaxRisk {
			continue
		}
		if len(cfg.IncludeCategories) > 0 {
			if _, ok := cfg.IncludeCategories[strings.ToLower(task.Category)]; !ok {
				continue
			}
		}
		if _, excluded := cfg.ExcludeIDs[strings.ToLower(task.ID)]; excluded {
			continue
		}
		if len(cfg.IncludeIDs) > 0 {
			if _, ok := cfg.IncludeIDs[strings.ToLower(task.ID)]; !ok {
				continue
			}
		}
		if overrides, ok := cfg.PathOverrides[task.ID]; ok && task.PathTask != nil {
			for idx, path := range overrides {
				cloned := task
				cloned.ID = fmt.Sprintf("%s-override-%d", task.ID, idx+1)
				cloned.PathTask = &PathTask{
					Path:            path,
					RemoveDirectory: task.PathTask.RemoveDirectory,
					MinAge:          task.PathTask.MinAge,
				}
				filtered = append(filtered, cloned)
			}
			continue
		}
		if roots, ok := cfg.PatternRoots[task.ID]; ok && task.PatternTask != nil {
			cloned := task
			cloned.PatternTask = &PatternTask{
				Roots:          roots,
				DirectoryNames: task.PatternTask.DirectoryNames,
				MinAge:         task.PatternTask.MinAge,
			}
			filtered = append(filtered, cloned)
			continue
		}
		filtered = append(filtered, task)
	}
	return filtered
}

func (e *Engine) plan(ctx context.Context, tasks []CleanupTask, cfg Config, processList map[string]struct{}) []PlanItem {
	type payload struct {
		index int
		item  PlanItem
	}
	workers := cfg.Parallelism
	if workers > len(tasks) && len(tasks) > 0 {
		workers = len(tasks)
	}
	if workers < 1 {
		workers = 1
	}
	out := make([]PlanItem, len(tasks))
	taskCh := make(chan int)
	resultCh := make(chan payload, len(tasks))
	wg := sync.WaitGroup{}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				resultCh <- payload{index: idx, item: evaluateTask(tasks[idx], cfg, processList)}
			}
		}()
	}

	for i := range tasks {
		taskCh <- i
	}
	close(taskCh)
	wg.Wait()
	close(resultCh)
	for result := range resultCh {
		out[result.index] = result.item
	}
	return out
}

func evaluateTask(task CleanupTask, cfg Config, processList map[string]struct{}) PlanItem {
	item := PlanItem{Task: task}
	if cfg.ProcessAware && len(task.ProcessHints) > 0 {
		if process := activeProcessForTask(task, processList); process != "" {
			item.SkippedReason = "process-running-" + process
			return item
		}
	}
	switch task.Kind {
	case TaskKindCommand:
		if _, err := exec.LookPath(task.CommandTask.Executable); err != nil {
			item.SkippedReason = "command-not-found"
			return item
		}
		item.Exists = true
		return item
	case TaskKindPath:
		if task.PathTask == nil || strings.TrimSpace(task.PathTask.Path) == "" {
			item.SkippedReason = "empty-path"
			return item
		}
		info, err := os.Stat(task.PathTask.Path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				item.SkippedReason = "path-not-found"
				return item
			}
			item.Err = err
			return item
		}
		if !info.IsDir() {
			item.SkippedReason = "not-directory"
			return item
		}
		item.Exists = true
		item.EstimatedSize, item.Err = directorySize(task.PathTask.Path, cfg.MinAge)
		return item
	case TaskKindPattern:
		if task.PatternTask == nil || len(task.PatternTask.Roots) == 0 {
			item.SkippedReason = "no-pattern-roots-configured"
			return item
		}
		targets, size, err := discoverPatternTargets(task.PatternTask.Roots, task.PatternTask.DirectoryNames, cfg.MinAge)
		if err != nil {
			item.Err = err
			return item
		}
		if len(targets) == 0 {
			item.SkippedReason = "no-pattern-matches"
			return item
		}
		item.Exists = true
		item.EstimatedSize = size
		paths := make([]string, 0, len(targets))
		for _, target := range targets {
			paths = append(paths, target)
		}
		item.Task.PathTask = &PathTask{Path: "", RemoveDirectory: false}
		item.Task.Description = strings.Join(paths, ";")
		return item
	default:
		item.SkippedReason = "unknown-task-kind"
		return item
	}
}

func (e *Engine) execute(ctx context.Context, plan []PlanItem, cfg Config) []ExecutionResult {
	results := make([]ExecutionResult, len(plan))
	for i, item := range plan {
		results[i] = ExecutionResult{
			Task:      item.Task,
			Skipped:   true,
			Attempted: false,
		}
		if item.Err != nil || item.SkippedReason != "" || !item.Exists {
			continue
		}
		results[i].Skipped = false
		if !cfg.AssumeYes && e.prompt != nil {
			msg := fmt.Sprintf("Execute cleanup [%s] (%s, %s)?", item.Task.Name, item.Task.Category, item.Task.Risk.String())
			if !e.prompt.Confirm(msg) {
				results[i].Skipped = true
				continue
			}
		}
		start := time.Now()
		results[i].Attempted = true
		if cfg.DryRun {
			results[i].DeletedBytes = item.EstimatedSize
			results[i].Duration = time.Since(start)
			continue
		}
		var err error
		deleteMode := normalizeDeleteMode(cfg.DeleteMode)
		switch item.Task.Kind {
		case TaskKindPath:
			results[i].DeletedItems, err = cleanupDirectoryWithMode(item.Task.PathTask.Path, cfg.MinAge, deleteMode, cfg.QuarantineDir)
			results[i].DeletedBytes = item.EstimatedSize
		case TaskKindPattern:
			paths := strings.Split(item.Task.Description, ";")
			results[i].DeletedItems, err = cleanupMatchedDirectoriesWithMode(paths, deleteMode, cfg.QuarantineDir)
			results[i].DeletedBytes = item.EstimatedSize
		case TaskKindCommand:
			err = runCommand(ctx, item.Task.CommandTask.Executable, item.Task.CommandTask.Args...)
		}
		if err != nil {
			// Never report reclaimed bytes for failed operations.
			results[i].DeletedBytes = 0
		}
		results[i].Err = err
		results[i].Duration = time.Since(start)
	}
	return results
}

func normalizeDeleteMode(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "delete"
	}
	if raw == "quarantine" {
		return "quarantine"
	}
	return "delete"
}

func directorySize(root string, minAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-minAge)
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if minAge > 0 && info.ModTime().After(cutoff) {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func cleanupDirectory(root string, minAge time.Duration) (int, error) {
	return cleanupDirectoryWithMode(root, minAge, "delete", "")
}

func cleanupDirectoryWithMode(root string, minAge time.Duration, mode string, quarantineDir string) (int, error) {
	if !isSafeCleanupPath(root) {
		return 0, fmt.Errorf("unsafe cleanup path rejected: %s", root)
	}
	items, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-minAge)
	removed := 0
	for _, item := range items {
		target := filepath.Join(root, item.Name())
		if minAge > 0 {
			info, statErr := item.Info()
			if statErr == nil && info.ModTime().After(cutoff) {
				continue
			}
		}
		if mode == "quarantine" {
			if _, err := movePathToQuarantine(target, quarantineDir); err != nil {
				return removed, err
			}
		} else {
			if err := os.RemoveAll(target); err != nil {
				return removed, err
			}
		}
		removed++
	}
	return removed, nil
}

func cleanupMatchedDirectories(paths []string) (int, error) {
	return cleanupMatchedDirectoriesWithMode(paths, "delete", "")
}

func cleanupMatchedDirectoriesWithMode(paths []string, mode string, quarantineDir string) (int, error) {
	removed := 0
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !isSafeCleanupPath(path) {
			return removed, fmt.Errorf("unsafe cleanup path rejected: %s", path)
		}
		if mode == "quarantine" {
			if _, err := movePathToQuarantine(path, quarantineDir); err != nil {
				return removed, err
			}
		} else {
			if err := os.RemoveAll(path); err != nil {
				return removed, err
			}
		}
		removed++
	}
	return removed, nil
}

func movePathToQuarantine(path string, quarantineDir string) (string, error) {
	return cleanup.MovePathToQuarantine(path, quarantineDir, "cleanup")
}

func discoverPatternTargets(roots []string, names []string, minAge time.Duration) ([]string, int64, error) {
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	matches := make([]string, 0, 64)
	var total int64
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if _, ok := nameSet[strings.ToLower(d.Name())]; !ok {
				return nil
			}
			size, sizeErr := directorySize(path, minAge)
			if sizeErr != nil {
				return nil
			}
			matches = append(matches, path)
			total += size
			return filepath.SkipDir
		})
		if err != nil {
			return nil, 0, err
		}
	}
	return matches, total, nil
}

func isSafeCleanupPath(path string) bool {
	cleaned := filepath.Clean(path)
	volume := filepath.VolumeName(cleaned)
	root := volume + string(filepath.Separator)
	if strings.EqualFold(cleaned, root) || cleaned == string(filepath.Separator) || cleaned == "." {
		return false
	}
	return len(cleaned) > len(root)
}

func runCommand(ctx context.Context, executable string, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
