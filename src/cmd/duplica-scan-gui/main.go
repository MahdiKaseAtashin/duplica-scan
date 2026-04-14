//go:build gui && cgo
// +build gui,cgo

package main

import (
	"context"
	_ "embed"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"duplica-scan/src/internal/buildinfo"
	"duplica-scan/src/internal/devcleanup"
	"duplica-scan/src/internal/duplicates"
	"duplica-scan/src/internal/hash"
	"duplica-scan/src/internal/report"
	"duplica-scan/src/internal/scanner"
	"duplica-scan/src/internal/selection"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed logo.png
var appLogoPNG []byte

func main() {
	a := app.New()
	logoResource := fyne.NewStaticResource("duplica-scan-logo.png", appLogoPNG)
	a.SetIcon(logoResource)
	w := a.NewWindow(fmt.Sprintf("Duplica Scan %s", buildinfo.Version))
	w.SetIcon(logoResource)
	w.Resize(fyne.NewSize(980, 760))

	var scanView fyne.CanvasObject
	var content *fyne.Container

	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("Choose a folder to scan")

	hashWorkersEntry := widget.NewEntry()
	hashWorkersEntry.SetText(strconv.Itoa(runtime.NumCPU()))

	excludeExtsEntry := widget.NewEntry()
	excludeExtsEntry.SetPlaceHolder("Example: .log,.tmp")

	excludeDirsEntry := widget.NewEntry()
	excludeDirsEntry.SetPlaceHolder("Example: node_modules,.git")

	minSizeEntry := widget.NewEntry()
	minSizeEntry.SetText("0")
	maxSizeEntry := widget.NewEntry()
	maxSizeEntry.SetText("0")

	dryRunCheck := widget.NewCheck("Dry run (no deletion)", nil)
	dryRunCheck.SetChecked(false)

	autoSelectSelect := widget.NewSelect([]string{"none", "newest", "oldest"}, nil)
	autoSelectSelect.SetSelected("none")
	matchModeSelect := widget.NewSelect([]string{
		"Same content",
		"Same file name",
		"Same file name and content",
		"Same file size (fast)",
	}, nil)
	matchModeSelect.SetSelected("Same content")

	exportFormatSelect := widget.NewSelect([]string{"none", "csv", "json"}, nil)
	exportFormatSelect.SetSelected("none")

	exportPathEntry := widget.NewEntry()
	exportPathEntry.SetPlaceHolder("Where to save the report")

	statusLabel := widget.NewLabel("Ready")
	stepLabel := widget.NewLabel("")
	stepLabel.Hide()
	scanProgress := widget.NewProgressBarInfinite()
	scanProgress.Hide()
	hashProgress := widget.NewProgressBar()
	hashProgress.Hide()
	detailLabel := widget.NewLabel("")
	detailLabel.Hide()

	output := widget.NewMultiLineEntry()
	output.Wrapping = fyne.TextWrapWord
	output.Disable()

	appendOutput := func(text string) {
		fyne.Do(func() {
			output.SetText(output.Text + text + "\n")
		})
	}

	browseBtn := widget.NewButton("Choose Folder", func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri == nil {
				return
			}
			pathEntry.SetText(uri.Path())
		}, w).Show()
	})

	var runBtn *widget.Button
	runBtn = widget.NewButton("Start Scan", func() {
		rootPath := strings.TrimSpace(pathEntry.Text)
		if rootPath == "" {
			dialog.ShowInformation("Missing path", "Please choose a directory or drive root to scan.", w)
			return
		}

		hashWorkers, err := parseInt(hashWorkersEntry.Text, runtime.NumCPU())
		if err != nil || hashWorkers < 1 {
			dialog.ShowInformation("Invalid hash workers", "Hash workers must be a positive integer.", w)
			return
		}
		minSize, err := parseInt64(minSizeEntry.Text, 0)
		if err != nil || minSize < 0 {
			dialog.ShowInformation("Invalid min size", "Min size must be a non-negative integer.", w)
			return
		}
		maxSize, err := parseInt64(maxSizeEntry.Text, 0)
		if err != nil || maxSize < 0 {
			dialog.ShowInformation("Invalid max size", "Max size must be a non-negative integer.", w)
			return
		}
		if maxSize > 0 && minSize > maxSize {
			dialog.ShowInformation("Invalid size range", "Min size cannot be greater than max size.", w)
			return
		}

		autoSelectRaw := strings.TrimSpace(autoSelectSelect.Selected)
		if autoSelectRaw == "none" {
			autoSelectRaw = ""
		}
		strategy, err := selection.NormalizeStrategy(autoSelectRaw)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		exportFormat := strings.TrimSpace(exportFormatSelect.Selected)
		if exportFormat == "none" {
			exportFormat = ""
		}
		exportPath := strings.TrimSpace(exportPathEntry.Text)
		if exportFormat != "" && exportPath == "" {
			exportPath = defaultExportPath(exportFormat)
		}

		output.SetText("")
		fyne.Do(func() {
			statusLabel.SetText("Running scan…")
			stepLabel.SetText("Step 1 of 2 · Scanning filesystem")
			stepLabel.Show()
			detailLabel.SetText("Files found: 0")
			detailLabel.Show()
			scanProgress.Show()
			hashProgress.Hide()
			hashProgress.SetValue(0)
		})
		runBtn.Disable()

		go func() {
			start := time.Now()
			filterOptions := scanner.ScanOptions{
				ExcludeExtensions: parseExtensions(excludeExtsEntry.Text),
				ExcludeDirs:       parseNames(excludeDirsEntry.Text),
				MinSizeBytes:      minSize,
				MaxSizeBytes:      maxSize,
			}

			var lastScanUpdate time.Time
			scanSummary, scanErr := scanner.ScanWithOptions(rootPath, func(p scanner.Progress) {
				now := time.Now()
				if now.Sub(lastScanUpdate) < 40*time.Millisecond && p.FilesSeen%50 != 0 {
					return
				}
				lastScanUpdate = now
				cur := p.Current
				if len(cur) > 72 {
					cur = "…" + cur[len(cur)-69:]
				}
				fyne.Do(func() {
					detailLabel.SetText(fmt.Sprintf("Files found: %d · %s", p.FilesSeen, cur))
				})
			}, filterOptions)
			if scanErr != nil {
				fyne.Do(func() {
					scanProgress.Hide()
					hashProgress.Hide()
					stepLabel.Hide()
					detailLabel.Hide()
					runBtn.Enable()
					statusLabel.SetText("Scan failed")
					dialog.ShowError(scanErr, w)
				})
				return
			}

			appendOutput(fmt.Sprintf("Scanned files: %d", len(scanSummary.Files)))

			fyne.Do(func() {
				stepLabel.SetText("Step 2 of 2 · Hashing candidate files")
				scanProgress.Hide()
				hashProgress.Show()
				hashProgress.SetValue(0)
				detailLabel.SetText("Preparing hash…")
			})

			groups, hashErrors := duplicates.DetectWithOptions(
				scanSummary.Files,
				hash.SHA256File,
				func(p duplicates.Progress) {
					fyne.Do(func() {
						if p.TotalToHash > 0 {
							hashProgress.SetValue(float64(p.HashedFiles) / float64(p.TotalToHash))
						}
						cur := p.CurrentPath
						if len(cur) > 64 {
							cur = "…" + cur[len(cur)-61:]
						}
						detailLabel.SetText(fmt.Sprintf("Hashed %d / %d · %s", p.HashedFiles, p.TotalToHash, cur))
					})
				},
				duplicates.DetectOptions{
					HashWorkers: hashWorkers,
					MatchMode:   duplicateMatchModeFromLabel(matchModeSelect.Selected),
				},
			)
			fyne.Do(func() {
				hashProgress.SetValue(1)
			})

			appendOutput(fmt.Sprintf("Duplicate groups found: %d", len(groups)))
			appendOutput(fmt.Sprintf("Scanner non-fatal errors: %d", len(scanSummary.Errors)))
			appendOutput(fmt.Sprintf("Hash non-fatal errors: %d", len(hashErrors)))
			appendOutput("")
			appendOutput(renderGroupsPreview(groups, 8, 6))

			initialSelection := make(map[string]struct{})
			if strategy != selection.StrategyManual {
				for _, path := range selection.AutoSelect(groups, strategy) {
					initialSelection[path] = struct{}{}
				}
				appendOutput(fmt.Sprintf("Auto-select (%s) picked %d file(s).", strategy, len(initialSelection)))
			}

			sorted := append([]duplicates.Group(nil), groups...)
			sort.Slice(sorted, func(i, j int) bool {
				if sorted[i].Size == sorted[j].Size {
					return sorted[i].Hash < sorted[j].Hash
				}
				return sorted[i].Size > sorted[j].Size
			})

			onBack := func() {
				fyne.Do(func() {
					if content != nil {
						content.Objects = []fyne.CanvasObject{scanView}
						content.Refresh()
					}
					statusLabel.SetText("Ready")
				})
			}
			resultsView := buildResultsView(w, onBack, groups, sorted, dryRunCheck.Checked, initialSelection, appendOutput)

			if exportFormat != "" {
				if err := report.Export(groups, exportFormat, exportPath); err != nil {
					appendOutput(fmt.Sprintf("Export failed: %v", err))
				} else {
					appendOutput(fmt.Sprintf("Report exported: %s", exportPath))
				}
			}

			fyne.Do(func() {
				scanProgress.Hide()
				hashProgress.Hide()
				stepLabel.Hide()
				detailLabel.Hide()
				runBtn.Enable()
				statusLabel.SetText(fmt.Sprintf("Done in %s", time.Since(start).Round(time.Millisecond)))
				if content != nil {
					content.Objects = []fyne.CanvasObject{resultsView}
					content.Refresh()
				}
			})
		}()
	})

	duplicateGeneralForm := widget.NewForm(
		widget.NewFormItem("Dry run", dryRunCheck),
		widget.NewFormItem("Scan speed", hashWorkersEntry),
		widget.NewFormItem("How to detect duplicates", matchModeSelect),
		widget.NewFormItem("Auto pick files to remove", autoSelectSelect),
	)
	duplicateFilterForm := widget.NewForm(
		widget.NewFormItem("Skip file types", excludeExtsEntry),
		widget.NewFormItem("Skip folders", excludeDirsEntry),
		widget.NewFormItem("Minimum file size (bytes)", minSizeEntry),
		widget.NewFormItem("Maximum file size (bytes)", maxSizeEntry),
	)
	duplicateOutputForm := widget.NewForm(
		widget.NewFormItem("Report type", exportFormatSelect),
		widget.NewFormItem("Save report to", exportPathEntry),
	)
	duplicateSettingsTabs := container.NewAppTabs(
		container.NewTabItem("General", container.NewPadded(duplicateGeneralForm)),
		container.NewTabItem("Filters", container.NewPadded(duplicateFilterForm)),
		container.NewTabItem("Output", container.NewPadded(duplicateOutputForm)),
	)
	duplicateSettingsTabs.SetTabLocation(container.TabLocationTop)

	scanView = container.NewBorder(
		container.NewVBox(
			widget.NewLabel(fmt.Sprintf("Duplica Scan GUI %s", buildinfo.Version)),
			widget.NewLabel("Quick mode: choose a folder and start."),
			container.NewBorder(nil, nil, nil, browseBtn, pathEntry),
			runBtn,
			statusLabel,
			stepLabel,
			scanProgress,
			hashProgress,
			detailLabel,
		),
		nil,
		nil,
		nil,
		container.NewVScroll(output),
	)
	cleanupView, cleanupSettingsTabs := buildCleanupView(w)
	content = container.NewMax(scanView)

	var duplicateTabBtn *widget.Button
	var cleanupTabBtn *widget.Button
	updateTab := func(tab string) {
		if tab == "cleanup" {
			content.Objects = []fyne.CanvasObject{cleanupView}
			duplicateTabBtn.Importance = widget.MediumImportance
			cleanupTabBtn.Importance = widget.HighImportance
		} else {
			content.Objects = []fyne.CanvasObject{scanView}
			duplicateTabBtn.Importance = widget.HighImportance
			cleanupTabBtn.Importance = widget.MediumImportance
		}
		content.Refresh()
		duplicateTabBtn.Refresh()
		cleanupTabBtn.Refresh()
	}
	duplicateTabBtn = widget.NewButton("Duplicate Files", func() {
		updateTab("duplicate")
	})
	cleanupTabBtn = widget.NewButton("Cleanup", func() {
		updateTab("cleanup")
	})
	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		openSettingsHub(duplicateSettingsTabs, cleanupSettingsTabs, updateTab)
	})
	settingsBtn.Importance = widget.MediumImportance

	topRow := container.NewBorder(
		nil,
		widget.NewSeparator(),
		container.NewHBox(duplicateTabBtn, cleanupTabBtn),
		settingsBtn,
		nil,
	)
	updateTab("duplicate")
	w.SetContent(container.NewBorder(topRow, nil, nil, nil, content))
	w.ShowAndRun()
}

func parseInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

func duplicateMatchModeFromLabel(label string) duplicates.MatchMode {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "same file name":
		return duplicates.MatchModeName
	case "same file name and content":
		return duplicates.MatchModeNameContent
	case "same file size (fast)":
		return duplicates.MatchModeSize
	default:
		return duplicates.MatchModeContent
	}
}

func openSettingsHub(duplicateSettings fyne.CanvasObject, cleanupSettings fyne.CanvasObject, updateTab func(string)) {
	hub := fyne.CurrentApp().NewWindow("Settings")
	hub.Resize(fyne.NewSize(900, 620))

	landingSelect := widget.NewSelect([]string{"Duplicate Files", "Cleanup"}, nil)
	landingSelect.SetSelected("Duplicate Files")
	themeSelect := widget.NewSelect([]string{"System", "Dark", "Light"}, nil)
	themeSelect.SetSelected("System")
	applyGeneralBtn := widget.NewButton("Save Changes", func() {
		switch themeSelect.Selected {
		case "Dark":
			fyne.CurrentApp().Settings().SetTheme(theme.DarkTheme())
		case "Light":
			fyne.CurrentApp().Settings().SetTheme(theme.LightTheme())
		default:
			// Keep default/system theme behavior by re-applying current app theme.
			fyne.CurrentApp().Settings().SetTheme(theme.DefaultTheme())
		}
		if landingSelect.Selected == "Cleanup" {
			updateTab("cleanup")
		} else {
			updateTab("duplicate")
		}
	})
	generalForm := widget.NewForm(
		widget.NewFormItem("Open this page first", landingSelect),
		widget.NewFormItem("Theme", themeSelect),
	)
	generalSection := container.NewVBox(
		widget.NewLabel("General Settings"),
		widget.NewLabel("Change basic app behavior and look."),
		generalForm,
		container.NewHBox(layout.NewSpacer(), applyGeneralBtn),
	)

	rootTabs := container.NewAppTabs(
		container.NewTabItem("General", container.NewPadded(generalSection)),
		container.NewTabItem("Duplicate Files", duplicateSettings),
		container.NewTabItem("Cleanup", cleanupSettings),
	)
	rootTabs.SetTabLocation(container.TabLocationTop)

	hub.SetContent(container.NewBorder(
		nil,
		container.NewPadded(container.NewHBox(layout.NewSpacer(), widget.NewButton("Done", func() { hub.Close() }))),
		nil,
		nil,
		rootTabs,
	))
	hub.Show()
}

func parseInt64(raw string, fallback int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func defaultExportPath(format string) string {
	base := fmt.Sprintf("duplicate-report-%s", time.Now().Format("20060102-150405"))
	switch strings.ToLower(strings.TrimSpace(format)) {
	case report.FormatCSV:
		return filepath.Join(".", "reports", base+".csv")
	case report.FormatJSON:
		return filepath.Join(".", "reports", base+".json")
	default:
		return filepath.Join(".", "reports", base+".txt")
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func parseExtensions(raw string) map[string]struct{} {
	return parseCSVSet(raw, func(v string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return ""
		}
		if !strings.HasPrefix(v, ".") {
			return "." + v
		}
		return v
	})
}

func parseNames(raw string) map[string]struct{} {
	return parseCSVSet(raw, func(v string) string {
		return strings.ToLower(strings.TrimSpace(v))
	})
}

func parseCSVSet(raw string, normalizer func(string) string) map[string]struct{} {
	result := make(map[string]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result
	}
	for _, part := range strings.Split(raw, ",") {
		value := normalizer(part)
		if value == "" {
			continue
		}
		result[value] = struct{}{}
	}
	return result
}

func renderGroupsPreview(groups []duplicates.Group, maxGroups int, maxFilesPerGroup int) string {
	if len(groups) == 0 {
		return "No duplicates found."
	}
	if maxGroups < 1 {
		maxGroups = 1
	}
	if maxFilesPerGroup < 1 {
		maxFilesPerGroup = 1
	}
	var b strings.Builder
	limit := len(groups)
	if limit > maxGroups {
		limit = maxGroups
	}
	for i, group := range groups[:limit] {
		b.WriteString(fmt.Sprintf("Group %d | size: %d | hash: %s\n", i+1, group.Size, group.Hash))
		fileLimit := len(group.Files)
		if fileLimit > maxFilesPerGroup {
			fileLimit = maxFilesPerGroup
		}
		for _, file := range group.Files[:fileLimit] {
			b.WriteString(fmt.Sprintf("  - %s | %s | %d\n", file.Name, file.Path, file.Size))
		}
		if len(group.Files) > fileLimit {
			b.WriteString(fmt.Sprintf("  ... %d more files in this group\n", len(group.Files)-fileLimit))
		}
		b.WriteString("\n")
	}
	if len(groups) > limit {
		b.WriteString(fmt.Sprintf("Showing %d of %d groups in log preview.\n", limit, len(groups)))
		b.WriteString("Use Export for the full report.\n")
	}
	return b.String()
}

func buildCleanupView(parent fyne.Window) (fyne.CanvasObject, fyne.CanvasObject) {
	var mainView fyne.CanvasObject
	var cleanupRoot *fyne.Container

	riskSelect := widget.NewSelect([]string{"safe", "moderate", "aggressive"}, nil)
	riskSelect.SetSelected("safe")
	dryRun := widget.NewCheck("Dry run (recommended)", nil)
	dryRun.SetChecked(false)
	processAware := widget.NewCheck("Skip cleanup when related apps are running", nil)
	processAware.SetChecked(true)
	assumeYes := widget.NewCheck("Assume yes (no per-task confirmations)", nil)
	assumeYes.SetChecked(false)

	parallelism := widget.NewEntry()
	parallelism.SetText(strconv.Itoa(runtime.NumCPU()))
	minAgeHours := widget.NewEntry()
	minAgeHours.SetText("24")

	includeCategories := widget.NewEntry()
	includeCategories.SetPlaceHolder("Example: os-temp,package-manager")
	includeIDs := widget.NewEntry()
	includeIDs.SetPlaceHolder("Optional: specific cleanup tasks")
	excludeIDs := widget.NewEntry()
	excludeIDs.SetPlaceHolder("Optional: tasks to skip")
	patternRoots := widget.NewEntry()
	patternRoots.SetPlaceHolder("Example: project-build-artifacts=D:/Projects")

	reportFormat := widget.NewSelect([]string{"none", "json", "md", "html"}, nil)
	reportFormat.SetSelected("none")
	reportPath := widget.NewEntry()
	reportPath.SetPlaceHolder("Where to save the cleanup report")
	targetPathEntry := widget.NewEntry()
	targetPathEntry.SetPlaceHolder("Optional: choose a folder to focus cleanup")
	targetBrowseBtn := widget.NewButton("Choose Folder", func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, parent)
				return
			}
			if uri == nil {
				return
			}
			targetPathEntry.SetText(uri.Path())
		}, parent).Show()
	})

	status := widget.NewLabel("Ready")
	output := widget.NewMultiLineEntry()
	output.Wrapping = fyne.TextWrapWord
	output.Disable()
	lastReport := devcleanup.RunReport{}
	hasReport := false

	appendOutput := func(text string) {
		fyne.Do(func() {
			output.SetText(output.Text + text + "\n")
		})
	}

	var runBtn *widget.Button
	runBtn = widget.NewButton("Start Cleanup", func() {
		p, err := parseInt(parallelism.Text, runtime.NumCPU())
		if err != nil || p < 1 {
			dialog.ShowInformation("Invalid parallelism", "Parallelism must be a positive integer.", parent)
			return
		}
		minHours, err := parseInt(minAgeHours.Text, 24)
		if err != nil || minHours < 0 {
			dialog.ShowInformation("Invalid min age", "Min age hours must be zero or positive.", parent)
			return
		}

		cfg := devcleanup.Config{
			MaxRisk:             devcleanup.ParseRiskLevel(strings.TrimSpace(strings.ToLower(riskSelect.Selected))),
			DryRun:              dryRun.Checked,
			AssumeYes:           assumeYes.Checked,
			Verbose:             false,
			DisableCommandTasks: true,
			Parallelism:         p,
			MinAge:              time.Duration(minHours) * time.Hour,
			ProcessAware:        processAware.Checked,
			IncludeCategories:   parseNames(includeCategories.Text),
			IncludeIDs:          parseNames(includeIDs.Text),
			ExcludeIDs:          parseNames(excludeIDs.Text),
			PathOverrides:       map[string][]string{},
			PatternRoots:        parsePatternRootsArg(patternRoots.Text),
		}
		targetPath := strings.TrimSpace(targetPathEntry.Text)
		if targetPath != "" {
			if _, exists := cfg.PatternRoots["project-build-artifacts"]; !exists {
				cfg.PatternRoots["project-build-artifacts"] = []string{targetPath}
			}
		}

		env, err := guiEnvironment()
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		engine := devcleanup.NewEngine(
			devcleanup.BuiltinProviders(env),
			devcleanup.Logger{Out: os.Stdout, Verbose: false},
			nil,
		)

		output.SetText("")
		runBtn.Disable()
		status.SetText("Running cleanup...")
		appendOutput("Starting cleanup run")
		go func() {
			report, runErr := engine.Run(context.Background(), cfg)
			if runErr != nil {
				fyne.Do(func() {
					runBtn.Enable()
					status.SetText("Cleanup failed")
					dialog.ShowError(runErr, parent)
				})
				return
			}

			appendOutput(fmt.Sprintf("Planned tasks: %d | Attempted: %d | Skipped: %d", report.Planned, report.Attempted, report.Skipped))
			appendOutput(fmt.Sprintf("Reclaimed: %s", formatBytes(report.ReclaimedBytes)))
			appendOutput(fmt.Sprintf("Duration: %s", report.Duration.Round(time.Millisecond)))

			if reportFormat.Selected != "none" {
				path := strings.TrimSpace(reportPath.Text)
				if path == "" {
					path = defaultCleanupReportPath(reportFormat.Selected)
				}
				if err := writeCleanupReport(path, reportFormat.Selected, report); err != nil {
					appendOutput(fmt.Sprintf("Report write failed: %v", err))
				} else {
					appendOutput("Report exported: " + path)
				}
			}

			fyne.Do(func() {
				lastReport = report
				hasReport = true
				runBtn.Enable()
				status.SetText("Cleanup finished")
				showCleanupResults(
					parent,
					cleanupRoot,
					mainView,
					lastReport,
					output.Text,
				)
			})
		}()
	})

	quickTempBtn := widget.NewButton("Quick Temporary Files Cleanup", func() {
		includeCategories.SetText("os-temp")
		riskSelect.SetSelected("safe")
		dryRun.SetChecked(true)
	})

	cleanupGeneralForm := widget.NewForm(
		widget.NewFormItem("Dry run", dryRun),
		widget.NewFormItem("Cleanup level", riskSelect),
		widget.NewFormItem("Skip when apps are running", processAware),
		widget.NewFormItem("Skip confirmation prompts", assumeYes),
		widget.NewFormItem("Cleanup speed", parallelism),
		widget.NewFormItem("Only clean files older than (hours)", minAgeHours),
	)
	cleanupScopeForm := widget.NewForm(
		widget.NewFormItem("Include groups", includeCategories),
		widget.NewFormItem("Include task IDs", includeIDs),
		widget.NewFormItem("Exclude task IDs", excludeIDs),
		widget.NewFormItem("Build folders location", patternRoots),
	)
	cleanupOutputForm := widget.NewForm(
		widget.NewFormItem("Report type", reportFormat),
		widget.NewFormItem("Save report to", reportPath),
	)
	cleanupSettingsTabs := container.NewAppTabs(
		container.NewTabItem("General", container.NewPadded(cleanupGeneralForm)),
		container.NewTabItem("Scope", container.NewPadded(cleanupScopeForm)),
		container.NewTabItem("Output", container.NewPadded(cleanupOutputForm)),
	)
	cleanupSettingsTabs.SetTabLocation(container.TabLocationTop)

	mainView = container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Cleanup temporary and app cache files"),
			widget.NewLabel("Quick mode: optionally choose a folder, then start."),
			container.NewBorder(nil, nil, nil, targetBrowseBtn, targetPathEntry),
			container.NewHBox(
				runBtn,
				quickTempBtn,
				widget.NewButton("View Last Results", func() {
					if !hasReport {
						dialog.ShowInformation("No results yet", "Run cleanup first to view results.", parent)
						return
					}
					showCleanupResults(parent, cleanupRoot, mainView, lastReport, output.Text)
				}),
			),
			status,
		),
		nil,
		nil,
		nil,
		container.NewVScroll(output),
	)
	cleanupRoot = container.NewMax(mainView)
	return cleanupRoot, cleanupSettingsTabs
}

func showCleanupResults(parent fyne.Window, root *fyne.Container, backView fyne.CanvasObject, report devcleanup.RunReport, runLog string) {
	summary := widget.NewLabel(fmt.Sprintf(
		"Planned: %d | Attempted: %d | Skipped: %d | Reclaimed: %s | Duration: %s",
		report.Planned,
		report.Attempted,
		report.Skipped,
		formatBytes(report.ReclaimedBytes),
		report.Duration.Round(time.Millisecond),
	))
	summary.Wrapping = fyne.TextWrapWord

	type cleanupResultRow struct {
		Status    string
		Name      string
		Category  string
		Risk      string
		Items     string
		Reclaimed string
		Error     string
	}
	rows := make([]cleanupResultRow, 0, len(report.Results))
	for _, result := range report.Results {
		status := "skipped"
		if result.Attempted && result.Error == "" {
			status = "ok"
		}
		if result.Error != "" {
			status = "error"
		}
		rows = append(rows, cleanupResultRow{
			Status:    status,
			Name:      result.Name,
			Category:  result.Category,
			Risk:      result.Risk,
			Items:     strconv.Itoa(result.DeletedItems),
			Reclaimed: formatBytes(result.DeletedBytes),
			Error:     result.Error,
		})
	}
	if len(rows) == 0 {
		rows = append(rows, cleanupResultRow{
			Status:    "n/a",
			Name:      "No task results",
			Category:  "-",
			Risk:      "-",
			Items:     "-",
			Reclaimed: "-",
			Error:     "",
		})
	}

	makeCell := func(text string, width float32, bold bool) fyne.CanvasObject {
		label := widget.NewLabel(text)
		label.Wrapping = fyne.TextWrapOff
		label.Truncation = fyne.TextTruncateEllipsis
		label.TextStyle = fyne.TextStyle{Bold: bold}
		return container.NewGridWrap(fyne.NewSize(width, 24), label)
	}
	makeRow := func(cols ...fyne.CanvasObject) fyne.CanvasObject {
		return container.NewHBox(cols...)
	}
	header := makeRow(
		makeCell("Status", 82, true),
		makeCell("Name", 170, true),
		makeCell("Category", 110, true),
		makeCell("Risk", 90, true),
		makeCell("Items", 80, true),
		makeCell("Reclaimed", 115, true),
		makeCell("Error", 360, true),
	)
	tableRows := make([]fyne.CanvasObject, 0, len(rows)+1)
	tableRows = append(tableRows, header)
	for _, row := range rows {
		tableRows = append(tableRows, makeRow(
			makeCell(row.Status, 82, false),
			makeCell(row.Name, 170, false),
			makeCell(row.Category, 110, false),
			makeCell(row.Risk, 90, false),
			makeCell(row.Items, 80, false),
			makeCell(row.Reclaimed, 115, false),
			makeCell(row.Error, 360, false),
		))
	}
	tableView := container.NewVScroll(container.NewVBox(tableRows...))
	chartsView := buildCleanupChartsView(report)

	logOutput := widget.NewMultiLineEntry()
	logOutput.SetText(runLog)
	logOutput.Disable()
	logOutput.Wrapping = fyne.TextWrapWord

	backBtn := widget.NewButton("Back", func() {
		root.Objects = []fyne.CanvasObject{backView}
		root.Refresh()
	})
	contentSwitcher := container.NewMax(tableView)
	var tableViewBtn *widget.Button
	var chartsViewBtn *widget.Button
	switchToCharts := func() {
		contentSwitcher.Objects = []fyne.CanvasObject{chartsView}
	}
	switchToTable := func() {
		contentSwitcher.Objects = []fyne.CanvasObject{tableView}
	}
	applyView := func(view string) {
		if view == "charts" {
			switchToCharts()
			tableViewBtn.Importance = widget.MediumImportance
			chartsViewBtn.Importance = widget.HighImportance
		} else {
			switchToTable()
			tableViewBtn.Importance = widget.HighImportance
			chartsViewBtn.Importance = widget.MediumImportance
		}
		contentSwitcher.Refresh()
		tableViewBtn.Refresh()
		chartsViewBtn.Refresh()
	}
	tableViewBtn = widget.NewButtonWithIcon("", theme.ListIcon(), func() {
		applyView("table")
	})
	chartsViewBtn = widget.NewButton("📊", func() {
		applyView("charts")
	})
	tableViewBtn.Importance = widget.HighImportance
	chartsViewBtn.Importance = widget.MediumImportance

	resultSplit := container.NewVSplit(
		contentSwitcher,
		container.NewVScroll(logOutput),
	)
	resultSplit.Offset = 0.58

	resultView := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Cleanup Results"),
			summary,
			container.NewHBox(widget.NewLabel("View:"), tableViewBtn, chartsViewBtn),
		),
		container.NewVBox(backBtn),
		nil,
		nil,
		resultSplit,
	)
	applyView("table")
	root.Objects = []fyne.CanvasObject{resultView}
	root.Refresh()
}

func buildCleanupChartsView(report devcleanup.RunReport) fyne.CanvasObject {
	statusCounts := map[string]int{"ok": 0, "skipped": 0, "error": 0}
	categoryBytes := make(map[string]int64)
	maxCount := 1
	maxBytes := int64(1)
	for _, result := range report.Results {
		status := "skipped"
		if result.Attempted && result.Error == "" {
			status = "ok"
		}
		if result.Error != "" {
			status = "error"
		}
		statusCounts[status]++
		if statusCounts[status] > maxCount {
			maxCount = statusCounts[status]
		}
		categoryBytes[result.Category] += result.DeletedBytes
		if categoryBytes[result.Category] > maxBytes {
			maxBytes = categoryBytes[result.Category]
		}
	}

	statusSection := container.NewVBox(widget.NewLabel("Task results overview"))
	statusOrder := []struct {
		Name  string
		Color color.Color
	}{
		{Name: "ok", Color: color.RGBA{R: 76, G: 175, B: 80, A: 255}},
		{Name: "skipped", Color: color.RGBA{R: 33, G: 150, B: 243, A: 255}},
		{Name: "error", Color: color.RGBA{R: 239, G: 83, B: 80, A: 255}},
	}
	for _, item := range statusOrder {
		statusSection.Add(buildHorizontalBar(item.Name, int64(statusCounts[item.Name]), int64(maxCount), item.Color, fmt.Sprintf("%d", statusCounts[item.Name])))
	}

	categorySection := container.NewVBox(widget.NewLabel("Freed space by type"))
	categories := make([]string, 0, len(categoryBytes))
	for category := range categoryBytes {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	if len(categories) == 0 {
		categories = []string{"none"}
		categoryBytes["none"] = 0
	}
	for _, category := range categories {
		categorySection.Add(buildHorizontalBar(category, categoryBytes[category], maxBytes, color.RGBA{R: 33, G: 150, B: 243, A: 255}, formatBytes(categoryBytes[category])))
	}
	volumeSection := container.NewVBox(widget.NewLabel("Freed space by drive"))
	volumeKeys := make([]string, 0, len(report.FreedByVolume))
	maxVolumeBytes := int64(1)
	for volume, bytes := range report.FreedByVolume {
		volumeKeys = append(volumeKeys, volume)
		if bytes > maxVolumeBytes {
			maxVolumeBytes = bytes
		}
	}
	sort.Strings(volumeKeys)
	if len(volumeKeys) == 0 {
		volumeSection.Add(widget.NewLabel("No drive-level data available for this run."))
	} else {
		for _, volume := range volumeKeys {
			volumeSection.Add(buildHorizontalBar(volume, report.FreedByVolume[volume], maxVolumeBytes, color.RGBA{R: 156, G: 39, B: 176, A: 255}, formatBytes(report.FreedByVolume[volume])))
		}
	}

	return container.NewVScroll(container.NewVBox(
		statusSection,
		widget.NewSeparator(),
		categorySection,
		widget.NewSeparator(),
		volumeSection,
	))
}

func buildHorizontalBar(name string, value int64, max int64, barColor color.Color, valueLabel string) fyne.CanvasObject {
	if max <= 0 {
		max = 1
	}
	width := float32(520)
	fillWidth := float32(value) / float32(max) * width
	if fillWidth < 2 && value > 0 {
		fillWidth = 2
	}
	bg := canvas.NewRectangle(color.RGBA{R: 58, G: 63, B: 73, A: 255})
	bg.SetMinSize(fyne.NewSize(width, 16))
	fg := canvas.NewRectangle(barColor)
	fgContainer := container.NewGridWrap(fyne.NewSize(fillWidth, 16), fg)
	bar := container.NewStack(bg, fgContainer)
	leftLabel := widget.NewLabel(name)
	leftLabel.Alignment = fyne.TextAlignLeading
	left := container.NewGridWrap(fyne.NewSize(130, 24), leftLabel)
	rightLabel := widget.NewLabel(valueLabel)
	rightLabel.Alignment = fyne.TextAlignLeading
	right := container.NewGridWrap(fyne.NewSize(90, 24), rightLabel)
	barCell := container.NewGridWrap(fyne.NewSize(width, 24), container.NewCenter(bar))
	return container.NewBorder(nil, nil, left, right, barCell)
}

func parsePatternRootsArg(raw string) map[string][]string {
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
		sides := strings.SplitN(part, "=", 2)
		if len(sides) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(sides[0]))
		if key == "" {
			continue
		}
		paths := make([]string, 0, 4)
		for _, p := range strings.Split(sides[1], "|") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			paths = append(paths, p)
		}
		if len(paths) == 0 {
			continue
		}
		result[key] = paths
	}
	return result
}

func guiEnvironment() (devcleanup.Environment, error) {
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

func defaultCleanupReportPath(format string) string {
	base := fmt.Sprintf("dev-cleanup-report-%s", time.Now().Format("20060102-150405"))
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return filepath.Join(".", "reports", base+".json")
	case "md":
		return filepath.Join(".", "reports", base+".md")
	case "html":
		return filepath.Join(".", "reports", base+".html")
	default:
		return filepath.Join(".", "reports", base+".txt")
	}
}

func writeCleanupReport(path, format string, report devcleanup.RunReport) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return devcleanup.WriteJSONReport(path, report)
	case "md":
		return devcleanup.WriteMarkdownReport(path, report)
	case "html":
		return devcleanup.WriteHTMLReport(path, report)
	default:
		return fmt.Errorf("unsupported report format: %s", format)
	}
}

const (
	resultsGroupsPerPage    = 12
	resultsMaxFilesPerGroup = 80
)

// resultsTableRow is one logical row in the duplicate-files table.
type resultsTableRow struct {
	path         string
	name         string
	size         int64
	groupNum     int
	fileNum      int
	overflowNote string // non-empty => informational row (no checkbox)
}

func buildResultsView(
	parent fyne.Window,
	onBack func(),
	originalGroups []duplicates.Group,
	sortedGroups []duplicates.Group,
	dryRun bool,
	initialSelection map[string]struct{},
	appendOutput func(string),
) fyne.CanvasObject {
	totalGroupCount := len(sortedGroups)

	selected := make(map[string]struct{}, 512)
	for p := range initialSelection {
		selected[p] = struct{}{}
	}
	var pageRows []resultsTableRow

	totalFiles := 0
	totalReclaimable := int64(0)
	for _, g := range originalGroups {
		totalFiles += len(g.Files)
		if len(g.Files) > 1 {
			groupTotal := int64(0)
			keepLargest := int64(0)
			for _, file := range g.Files {
				groupTotal += file.Size
				if file.Size > keepLargest {
					keepLargest = file.Size
				}
			}
			if groupTotal > keepLargest {
				totalReclaimable += groupTotal - keepLargest
			}
		}
	}

	countLabel := widget.NewLabel("")
	summaryLabel := widget.NewLabel(
		fmt.Sprintf(
			"Groups: %d | Candidate files: %d | Estimated reclaimable: %s | Mode: %s",
			len(originalGroups),
			totalFiles,
			formatBytes(totalReclaimable),
			map[bool]string{true: "Dry run (scan only)", false: "Delete"}[dryRun],
		),
	)

	updateCount := func() {
		countLabel.SetText(fmt.Sprintf("Selected files: %d", len(selected)))
	}

	allPathsFromSorted := func() []string {
		paths := make([]string, 0, totalFiles)
		for _, g := range sortedGroups {
			for _, f := range g.Files {
				paths = append(paths, f.Path)
			}
		}
		return paths
	}

	currentPage := 0
	totalPages := (len(sortedGroups) + resultsGroupsPerPage - 1) / resultsGroupsPerPage
	if totalPages < 1 {
		totalPages = 1
	}

	pageLabel := widget.NewLabel("")
	firstBtn := widget.NewButton("First", func() {})
	prevBtn := widget.NewButton("Previous", func() {})
	nextBtn := widget.NewButton("Next", func() {})
	lastBtn := widget.NewButton("Last", func() {})

	var resultsTable *widget.Table

	rebuildPage := func() {
		start := currentPage * resultsGroupsPerPage
		end := start + resultsGroupsPerPage
		if end > len(sortedGroups) {
			end = len(sortedGroups)
		}

		pageLabel.SetText(fmt.Sprintf("Page %d / %d", currentPage+1, totalPages))
		firstBtn.Disable()
		prevBtn.Disable()
		nextBtn.Disable()
		lastBtn.Disable()
		if currentPage > 0 {
			firstBtn.Enable()
			prevBtn.Enable()
		}
		if currentPage < totalPages-1 {
			nextBtn.Enable()
			lastBtn.Enable()
		}

		pageRows = nil
		if len(sortedGroups) == 0 {
			if resultsTable != nil {
				resultsTable.Refresh()
			}
			return
		}

		globalIdx := start
		for _, group := range sortedGroups[start:end] {
			gnum := globalIdx + 1
			globalIdx++

			files := group.Files
			totalFilesInGroup := len(files)
			overflow := 0
			if len(files) > resultsMaxFilesPerGroup {
				overflow = len(files) - resultsMaxFilesPerGroup
				files = files[:resultsMaxFilesPerGroup]
			}

			for fi, file := range files {
				pageRows = append(pageRows, resultsTableRow{
					path:     file.Path,
					name:     file.Name,
					size:     file.Size,
					groupNum: gnum,
					fileNum:  fi + 1,
				})
			}
			if overflow > 0 {
				pageRows = append(pageRows, resultsTableRow{
					overflowNote: fmt.Sprintf(
						"… files %d–%d not shown (%d more; %d file(s) in this group — use Export for the full list).",
						resultsMaxFilesPerGroup+1, totalFilesInGroup, overflow, totalFilesInGroup,
					),
				})
			}
		}

		if resultsTable != nil {
			resultsTable.ScrollToTop()
			resultsTable.Refresh()
		}
	}

	createTableCell := func() fyne.CanvasObject {
		chk := widget.NewCheck("", nil)
		lab := widget.NewLabel("")
		lab.Wrapping = fyne.TextWrapOff
		lab.Truncation = fyne.TextTruncateEllipsis
		return container.NewStack(lab, chk)
	}

	updateTableCell := func(id widget.TableCellID, obj fyne.CanvasObject) {
		if id.Row < 0 || id.Col < 0 {
			return
		}
		if id.Row >= len(pageRows) {
			return
		}
		row := pageRows[id.Row]
		st := obj.(*fyne.Container)
		lab := st.Objects[0].(*widget.Label)
		chk := st.Objects[1].(*widget.Check)

		if row.overflowNote != "" {
			chk.Hide()
			lab.Show()
			switch id.Col {
			case 0, 1, 2, 3, 5:
				lab.SetText("")
			case 4:
				lab.Wrapping = fyne.TextWrapOff
				lab.Truncation = fyne.TextTruncateEllipsis
				lab.SetText(row.overflowNote)
			}
			return
		}

		path := row.path
		switch id.Col {
		case 0:
			lab.Hide()
			chk.Show()
			_, on := selected[path]
			chk.SetChecked(on)
			chk.OnChanged = func(on bool) {
				if on {
					selected[path] = struct{}{}
				} else {
					delete(selected, path)
				}
				updateCount()
			}
		case 1:
			chk.Hide()
			lab.Show()
			lab.Wrapping = fyne.TextWrapOff
			lab.Truncation = fyne.TextTruncateEllipsis
			lab.SetText(strconv.Itoa(row.fileNum))
		case 2:
			chk.Hide()
			lab.Show()
			lab.Wrapping = fyne.TextWrapOff
			lab.Truncation = fyne.TextTruncateEllipsis
			lab.SetText(fmt.Sprintf("%d / %d", row.groupNum, totalGroupCount))
		case 3:
			chk.Hide()
			lab.Show()
			lab.Wrapping = fyne.TextWrapOff
			lab.Truncation = fyne.TextTruncateEllipsis
			lab.SetText(row.name)
		case 4:
			chk.Hide()
			lab.Show()
			lab.Wrapping = fyne.TextWrapOff
			lab.Truncation = fyne.TextTruncateEllipsis
			lab.SetText(row.path)
		case 5:
			chk.Hide()
			lab.Show()
			lab.Wrapping = fyne.TextWrapOff
			lab.Truncation = fyne.TextTruncateEllipsis
			lab.SetText(formatBytes(row.size))
		}
	}

	resultsTable = widget.NewTable(
		func() (int, int) { return len(pageRows), 6 },
		createTableCell,
		updateTableCell,
	)
	resultsTable.ShowHeaderRow = true
	resultsTable.ShowHeaderColumn = false
	resultsTable.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		l := o.(*widget.Label)
		l.TextStyle = fyne.TextStyle{Bold: true}
		if id.Row != -1 || id.Col < 0 {
			return
		}
		headers := []string{"Select", "#", "Group", "Name", "Path", "Size"}
		if id.Col < len(headers) {
			l.SetText(headers[id.Col])
		}
	}
	resultsTable.SetColumnWidth(0, 72)
	resultsTable.SetColumnWidth(1, 40)
	resultsTable.SetColumnWidth(2, 88)
	resultsTable.SetColumnWidth(3, 160)
	resultsTable.SetColumnWidth(4, 320)
	resultsTable.SetColumnWidth(5, 112)

	syncVisibleChecks := func() {
		if resultsTable != nil {
			resultsTable.Refresh()
		}
		updateCount()
	}

	setSelection := func(paths []string) {
		selected = make(map[string]struct{}, len(paths))
		for _, p := range paths {
			selected[p] = struct{}{}
		}
		syncVisibleChecks()
	}

	selectAllBtn := widget.NewButton("Select All", func() {
		setSelection(allPathsFromSorted())
	})

	clearBtn := widget.NewButton("Clear", func() {
		setSelection(nil)
	})

	keepNewestBtn := widget.NewButton("Keep Newest", func() {
		setSelection(selection.AutoSelect(originalGroups, selection.StrategyNewest))
	})

	keepOldestBtn := widget.NewButton("Keep Oldest", func() {
		setSelection(selection.AutoSelect(originalGroups, selection.StrategyOldest))
	})

	deleteLabel := "Delete"

	confirmAndDelete := func() {
		if len(selected) == 0 {
			dialog.ShowInformation("No selection", "Select at least one file.", parent)
			return
		}
		paths := make([]string, 0, len(selected))
		for p := range selected {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		title := "Are you sure?"
		message := fmt.Sprintf(
			"This will permanently delete %d selected file(s) from disk. This cannot be undone.",
			len(paths),
		)
		dialog.NewConfirm(
			title,
			message,
			func(ok bool) {
				if !ok {
					return
				}
				n := len(paths)
				prog := widget.NewProgressBar()
				prog.Max = 1
				status := widget.NewLabel("")
				body := container.NewVBox(
					status,
					prog,
				)
				dlg := dialog.NewCustomWithoutButtons("Deleting files", body, parent)
				dlg.Show()

				go func() {
					failures := 0
					for i, path := range paths {
						idx := i + 1
						fyne.Do(func() {
							if n > 0 {
								prog.SetValue(float64(idx) / float64(n))
								status.SetText(fmt.Sprintf("Deleting %d of %d (%.0f%%)…", idx, n, 100*float64(idx)/float64(n)))
							}
						})
						err := os.Remove(path)
						if err != nil {
							failures++
							appendOutput(fmt.Sprintf("Failed: %s (%v)", path, err))
						}
					}
					fyne.Do(func() {
						dlg.Hide()
						appendOutput(fmt.Sprintf("Result action completed. Success: %d, Failed: %d", n-failures, failures))
						onBack()
					})
				}()
			},
			parent,
		).Show()
	}

	bottomDeleteBtn := widget.NewButton(deleteLabel, confirmAndDelete)
	bottomDeleteBtn.Importance = widget.DangerImportance

	exportCSVBtn := widget.NewButton("Export CSV", func() {
		path := defaultExportPath(report.FormatCSV)
		if err := report.Export(originalGroups, report.FormatCSV, path); err != nil {
			dialog.ShowError(err, parent)
			return
		}
		appendOutput("CSV exported: " + path)
		dialog.ShowInformation("Export complete", "CSV exported to:\n"+path, parent)
	})

	exportJSONBtn := widget.NewButton("Export JSON", func() {
		path := defaultExportPath(report.FormatJSON)
		if err := report.Export(originalGroups, report.FormatJSON, path); err != nil {
			dialog.ShowError(err, parent)
			return
		}
		appendOutput("JSON exported: " + path)
		dialog.ShowInformation("Export complete", "JSON exported to:\n"+path, parent)
	})

	firstBtn.OnTapped = func() {
		if currentPage > 0 {
			currentPage = 0
			rebuildPage()
		}
	}
	prevBtn.OnTapped = func() {
		if currentPage > 0 {
			currentPage--
			rebuildPage()
		}
	}
	nextBtn.OnTapped = func() {
		if currentPage < totalPages-1 {
			currentPage++
			rebuildPage()
		}
	}
	lastBtn.OnTapped = func() {
		if currentPage < totalPages-1 {
			currentPage = totalPages - 1
			rebuildPage()
		}
	}

	updateCount()

	toolbar := container.NewHBox(
		selectAllBtn, clearBtn, keepNewestBtn, keepOldestBtn,
		exportCSVBtn, exportJSONBtn,
	)
	toolbarScroll := container.NewHScroll(toolbar)

	paginationBar := container.NewHBox(
		layout.NewSpacer(),
		firstBtn,
		prevBtn,
		pageLabel,
		nextBtn,
		lastBtn,
		layout.NewSpacer(),
	)

	cancelBtn := widget.NewButton("Back to scan", func() {
		onBack()
	})
	actionBar := container.NewHBox(layout.NewSpacer(), cancelBtn, bottomDeleteBtn)

	bottomStack := container.NewVBox(
		paginationBar,
		actionBar,
	)

	top := container.NewVBox(
		widget.NewLabel("Review duplicates and choose actions"),
		summaryLabel,
		countLabel,
		toolbarScroll,
	)

	out := container.NewBorder(
		container.NewPadded(top),
		container.NewPadded(bottomStack),
		nil,
		nil,
		resultsTable,
	)
	rebuildPage()
	return out
}
