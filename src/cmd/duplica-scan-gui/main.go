//go:build gui && cgo
// +build gui,cgo

package main

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"duplica-scan/src/internal/buildinfo"
	"duplica-scan/src/internal/cleanup"
	"duplica-scan/src/internal/duplicates"
	"duplica-scan/src/internal/hash"
	"duplica-scan/src/internal/report"
	"duplica-scan/src/internal/scanner"
	"duplica-scan/src/internal/selection"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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

	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("Select directory or drive root")

	hashWorkersEntry := widget.NewEntry()
	hashWorkersEntry.SetText(strconv.Itoa(runtime.NumCPU()))

	excludeExtsEntry := widget.NewEntry()
	excludeExtsEntry.SetPlaceHolder(".log,.tmp")

	excludeDirsEntry := widget.NewEntry()
	excludeDirsEntry.SetPlaceHolder("node_modules,.git")

	minSizeEntry := widget.NewEntry()
	minSizeEntry.SetText("0")
	maxSizeEntry := widget.NewEntry()
	maxSizeEntry.SetText("0")

	dryRunCheck := widget.NewCheck("Dry run (no deletion)", nil)
	dryRunCheck.SetChecked(true)

	autoSelectSelect := widget.NewSelect([]string{"none", "newest", "oldest"}, nil)
	autoSelectSelect.SetSelected("none")

	exportFormatSelect := widget.NewSelect([]string{"none", "csv", "json"}, nil)
	exportFormatSelect.SetSelected("none")

	exportPathEntry := widget.NewEntry()
	exportPathEntry.SetPlaceHolder("./reports/duplicate-report-*.json")

	statusLabel := widget.NewLabel("Ready")
	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	output := widget.NewMultiLineEntry()
	output.Wrapping = fyne.TextWrapWord
	output.Disable()

	appendOutput := func(text string) {
		fyne.Do(func() {
			output.SetText(output.Text + text + "\n")
		})
	}

	browseBtn := widget.NewButton("Browse...", func() {
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
	runBtn = widget.NewButton("Run Scan", func() {
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
		statusLabel.SetText("Running scan...")
		progress.Show()
		runBtn.Disable()

		go func() {
			start := time.Now()
			filterOptions := scanner.ScanOptions{
				ExcludeExtensions: parseExtensions(excludeExtsEntry.Text),
				ExcludeDirs:       parseNames(excludeDirsEntry.Text),
				MinSizeBytes:      minSize,
				MaxSizeBytes:      maxSize,
			}

			scanSummary, scanErr := scanner.ScanWithOptions(rootPath, nil, filterOptions)
			if scanErr != nil {
				fyne.Do(func() {
					progress.Hide()
					runBtn.Enable()
					statusLabel.SetText("Scan failed")
					dialog.ShowError(scanErr, w)
				})
				return
			}

			appendOutput(fmt.Sprintf("Scanned files: %d", len(scanSummary.Files)))
			appendOutput("Hashing candidates...")

			groups, hashErrors := duplicates.DetectWithOptions(
				scanSummary.Files,
				hash.SHA256File,
				nil,
				duplicates.DetectOptions{HashWorkers: hashWorkers},
			)

			appendOutput(fmt.Sprintf("Duplicate groups found: %d", len(groups)))
			appendOutput(fmt.Sprintf("Scanner non-fatal errors: %d", len(scanSummary.Errors)))
			appendOutput(fmt.Sprintf("Hash non-fatal errors: %d", len(hashErrors)))
			appendOutput("")
			appendOutput(renderGroups(groups))

			initialSelection := make(map[string]struct{})
			if strategy != selection.StrategyManual {
				for _, path := range selection.AutoSelect(groups, strategy) {
					initialSelection[path] = struct{}{}
				}
				appendOutput(fmt.Sprintf("Auto-select (%s) picked %d file(s).", strategy, len(initialSelection)))
			}

			fyne.Do(func() {
				showResultsWindow(a, groups, dryRunCheck.Checked, initialSelection, appendOutput)
			})

			if exportFormat != "" {
				if err := report.Export(groups, exportFormat, exportPath); err != nil {
					appendOutput(fmt.Sprintf("Export failed: %v", err))
				} else {
					appendOutput(fmt.Sprintf("Report exported: %s", exportPath))
				}
			}

			fyne.Do(func() {
				progress.Hide()
				runBtn.Enable()
				statusLabel.SetText(fmt.Sprintf("Done in %s", time.Since(start).Round(time.Millisecond)))
			})
		}()
	})

	form := widget.NewForm(
		widget.NewFormItem("Path", container.NewBorder(nil, nil, nil, browseBtn, pathEntry)),
		widget.NewFormItem("Hash workers", hashWorkersEntry),
		widget.NewFormItem("Exclude extensions", excludeExtsEntry),
		widget.NewFormItem("Exclude directories", excludeDirsEntry),
		widget.NewFormItem("Min size bytes", minSizeEntry),
		widget.NewFormItem("Max size bytes", maxSizeEntry),
		widget.NewFormItem("Auto-select", autoSelectSelect),
		widget.NewFormItem("Export format", exportFormatSelect),
		widget.NewFormItem("Export path", exportPathEntry),
	)

	content := container.NewBorder(
		container.NewVBox(widget.NewLabel(fmt.Sprintf("Duplica Scan GUI %s", buildinfo.Version)), dryRunCheck, form, runBtn, statusLabel, progress),
		nil,
		nil,
		nil,
		container.NewVScroll(output),
	)
	w.SetContent(content)
	w.ShowAndRun()
}

func parseInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
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

func renderGroups(groups []duplicates.Group) string {
	if len(groups) == 0 {
		return "No duplicates found."
	}
	var b strings.Builder
	for i, group := range groups {
		b.WriteString(fmt.Sprintf("Group %d | size: %d | hash: %s\n", i+1, group.Size, group.Hash))
		for _, file := range group.Files {
			b.WriteString(fmt.Sprintf("  - %s | %s | %d\n", file.Name, file.Path, file.Size))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func showResultsWindow(
	a fyne.App,
	groups []duplicates.Group,
	dryRun bool,
	initialSelection map[string]struct{},
	appendOutput func(string),
) {
	win := a.NewWindow("Scan Results")
	win.Resize(fyne.NewSize(960, 720))

	selected := make(map[string]struct{}, 512)
	for p := range initialSelection {
		selected[p] = struct{}{}
	}
	checkByPath := make(map[string]*widget.Check, 512)

	countLabel := widget.NewLabel("")
	updateCount := func() {
		countLabel.SetText(fmt.Sprintf("Selected files: %d", len(selected)))
	}

	sortedGroups := append([]duplicates.Group(nil), groups...)
	sort.Slice(sortedGroups, func(i, j int) bool {
		if sortedGroups[i].Size == sortedGroups[j].Size {
			return sortedGroups[i].Hash < sortedGroups[j].Hash
		}
		return sortedGroups[i].Size > sortedGroups[j].Size
	})

	groupRows := make([]fyne.CanvasObject, 0, len(sortedGroups))
	for i, group := range sortedGroups {
		header := widget.NewLabel(fmt.Sprintf("Group %d | size: %d bytes | hash: %s", i+1, group.Size, group.Hash))
		fileRows := make([]fyne.CanvasObject, 0, len(group.Files))
		for _, file := range group.Files {
			path := file.Path
			check := widget.NewCheck(
				fmt.Sprintf("%s | %s | %d bytes", file.Name, path, file.Size),
				func(checked bool) {
					if checked {
						selected[path] = struct{}{}
					} else {
						delete(selected, path)
					}
					updateCount()
				},
			)
			if _, ok := selected[path]; ok {
				check.SetChecked(true)
			}
			checkByPath[path] = check
			fileRows = append(fileRows, check)
		}
		groupRows = append(groupRows, container.NewVBox(header, container.NewVBox(fileRows...)))
	}

	setSelection := func(paths []string) {
		selected = make(map[string]struct{}, len(paths))
		for _, check := range checkByPath {
			check.SetChecked(false)
		}
		for _, path := range paths {
			selected[path] = struct{}{}
			if check, ok := checkByPath[path]; ok {
				check.SetChecked(true)
			}
		}
		updateCount()
	}

	selectAllBtn := widget.NewButton("Select All", func() {
		paths := make([]string, 0, len(checkByPath))
		for path := range checkByPath {
			paths = append(paths, path)
		}
		setSelection(paths)
	})

	clearBtn := widget.NewButton("Clear", func() {
		setSelection(nil)
	})

	keepNewestBtn := widget.NewButton("Keep Newest", func() {
		setSelection(selection.AutoSelect(groups, selection.StrategyNewest))
	})

	keepOldestBtn := widget.NewButton("Keep Oldest", func() {
		setSelection(selection.AutoSelect(groups, selection.StrategyOldest))
	})

	deleteBtn := widget.NewButton("Delete Selected", func() {
		if len(selected) == 0 {
			dialog.ShowInformation("No selection", "Select at least one file.", win)
			return
		}
		paths := make([]string, 0, len(selected))
		for p := range selected {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		actionText := "delete"
		if dryRun {
			actionText = "simulate deletion for"
		}
		dialog.NewConfirm(
			"Confirm Action",
			fmt.Sprintf("Proceed to %s %d selected file(s)?", actionText, len(paths)),
			func(ok bool) {
				if !ok {
					return
				}
				results := cleanup.DeleteFiles(paths, dryRun)
				failures := 0
				for _, r := range results {
					if r.Err != nil {
						failures++
						appendOutput(fmt.Sprintf("Failed: %s (%v)", r.Path, r.Err))
					}
				}
				appendOutput(fmt.Sprintf("Result action completed. Success: %d, Failed: %d", len(results)-failures, failures))
				dialog.ShowInformation("Action complete", fmt.Sprintf("Success: %d, Failed: %d", len(results)-failures, failures), win)
			},
			win,
		).Show()
	})

	exportCSVBtn := widget.NewButton("Export CSV", func() {
		path := defaultExportPath(report.FormatCSV)
		if err := report.Export(groups, report.FormatCSV, path); err != nil {
			dialog.ShowError(err, win)
			return
		}
		appendOutput("CSV exported: " + path)
		dialog.ShowInformation("Export complete", "CSV exported to:\n"+path, win)
	})

	exportJSONBtn := widget.NewButton("Export JSON", func() {
		path := defaultExportPath(report.FormatJSON)
		if err := report.Export(groups, report.FormatJSON, path); err != nil {
			dialog.ShowError(err, win)
			return
		}
		appendOutput("JSON exported: " + path)
		dialog.ShowInformation("Export complete", "JSON exported to:\n"+path, win)
	})

	updateCount()
	toolbar := container.NewHBox(selectAllBtn, clearBtn, keepNewestBtn, keepOldestBtn, deleteBtn, exportCSVBtn, exportJSONBtn)
	content := container.NewBorder(
		container.NewVBox(widget.NewLabel("Review duplicates and choose actions"), countLabel, toolbar),
		nil,
		nil,
		nil,
		container.NewVScroll(container.NewVBox(groupRows...)),
	)
	win.SetContent(content)
	win.Show()
}
