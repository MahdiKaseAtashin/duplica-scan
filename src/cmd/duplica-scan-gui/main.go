//go:build gui
// +build gui

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

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

func main() {
	a := app.New()
	w := a.NewWindow("Duplica Scan")
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

	runBtn := widget.NewButton("Run Scan", func() {
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

			if exportFormat != "" {
				if err := report.Export(groups, exportFormat, exportPath); err != nil {
					appendOutput(fmt.Sprintf("Export failed: %v", err))
				} else {
					appendOutput(fmt.Sprintf("Report exported: %s", exportPath))
				}
			}

			selected := make([]string, 0)
			if strategy != selection.StrategyManual {
				selected = selection.AutoSelect(groups, strategy)
				appendOutput(fmt.Sprintf("Auto-select (%s) picked %d file(s).", strategy, len(selected)))
			}

			if !dryRunCheck.Checked && len(selected) > 0 {
				fyne.Do(func() {
					dialog.NewConfirm(
						"Confirm Deletion",
						fmt.Sprintf("Delete %d selected duplicate file(s)?", len(selected)),
						func(ok bool) {
							if !ok {
								appendOutput("Deletion canceled.")
								return
							}
							results := cleanup.DeleteFiles(selected, false)
							failures := 0
							for _, r := range results {
								if r.Err != nil {
									failures++
									appendOutput(fmt.Sprintf("Failed: %s (%v)", r.Path, r.Err))
								}
							}
							appendOutput(fmt.Sprintf("Deletion complete. Success: %d, Failed: %d", len(results)-failures, failures))
						},
						w,
					).Show()
				})
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
		container.NewVBox(widget.NewLabel("Duplica Scan GUI"), dryRunCheck, form, runBtn, statusLabel, progress),
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
