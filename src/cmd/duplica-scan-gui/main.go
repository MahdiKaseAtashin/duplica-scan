//go:build gui && cgo
// +build gui,cgo

package main

import (
	"context"
	"encoding/json"
	_ "embed"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"duplica-scan/src/internal/buildinfo"
	"duplica-scan/src/internal/cleanup"
	"duplica-scan/src/internal/devcleanup"
	"duplica-scan/src/internal/duplicates"
	"duplica-scan/src/internal/hash"
	"duplica-scan/src/internal/model"
	"duplica-scan/src/internal/report"
	"duplica-scan/src/internal/scanner"
	"duplica-scan/src/internal/selection"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
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
	var homeView fyne.CanvasObject
	var content *fyne.Container
	healthState, _ := loadGUIHealthState()
	if strings.TrimSpace(healthState.Accessibility.Language) == "" {
		healthState.Accessibility.Language = "en"
	}
	if strings.TrimSpace(healthState.Accessibility.TextScale) == "" {
		healthState.Accessibility.TextScale = "normal"
	}
	if strings.TrimSpace(healthState.Accessibility.ThemeMode) == "" {
		healthState.Accessibility.ThemeMode = "system"
	}
	if strings.TrimSpace(healthState.Accessibility.StartPage) == "" {
		healthState.Accessibility.StartPage = "Home"
	}
	applyAccessibilityTheme(a, healthState.Accessibility)

	cacheSizeLabel := widget.NewLabel(localize(healthState.Accessibility.Language, "cache_size") + ": ...")
	tempSizeLabel := widget.NewLabel(localize(healthState.Accessibility.Language, "temp_size") + ": ...")
	dupCountLabel := widget.NewLabel(fmt.Sprintf("%s: %d", localize(healthState.Accessibility.Language, "last_dups"), healthState.LastDuplicateGroups))
	lastCleanupLabel := widget.NewLabel(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + localize(healthState.Accessibility.Language, "never"))
	dashboardCharts := container.NewMax(widget.NewLabel("Charts loading..."))
	lastCacheBytes := int64(0)
	lastTempBytes := int64(0)
	if !healthState.LastCleanupAt.IsZero() {
		lastCleanupLabel.SetText(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + healthState.LastCleanupAt.Local().Format("2006-01-02 15:04"))
	}
	healthStatusLabel := widget.NewLabel("")

	refreshHealthCard := func() {
		healthStatusLabel.SetText("Refreshing...")
		go func() {
			cacheBytes, tempBytes, err := estimateSystemHealth()
			fyne.Do(func() {
				if err != nil {
					healthStatusLabel.SetText("Health refresh had partial errors: " + err.Error())
				} else {
					healthStatusLabel.SetText("Updated")
				}
				lastCacheBytes = cacheBytes
				lastTempBytes = tempBytes
				cacheSizeLabel.SetText(localize(healthState.Accessibility.Language, "cache_size") + ": " + formatBytes(cacheBytes))
				tempSizeLabel.SetText(localize(healthState.Accessibility.Language, "temp_size") + ": " + formatBytes(tempBytes))
				dupCountLabel.SetText(fmt.Sprintf("%s: %d", localize(healthState.Accessibility.Language, "last_dups"), healthState.LastDuplicateGroups))
				if healthState.LastCleanupAt.IsZero() {
					lastCleanupLabel.SetText(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + localize(healthState.Accessibility.Language, "never"))
				} else {
					lastCleanupLabel.SetText(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + healthState.LastCleanupAt.Local().Format("2006-01-02 15:04"))
				}
				dashboardCharts.Objects = []fyne.CanvasObject{
					buildDashboardMiniCharts(lastCacheBytes, lastTempBytes, healthState.LastDuplicateGroups, healthState.LastCleanupAt),
				}
				dashboardCharts.Refresh()
			})
		}()
	}

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
	deleteModeSelect := widget.NewSelect([]string{"Delete permanently", "Move to safety backup"}, nil)
	deleteModeSelect.SetSelected("Delete permanently")
	quarantinePathEntry := widget.NewEntry()
	quarantinePathEntry.SetPlaceHolder("Optional safety backup folder")

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
			healthState.LastDuplicateGroups = len(groups)
			healthState.LastDuplicateAt = time.Now()
			_ = saveGUIHealthState(healthState)
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
						content.Objects = []fyne.CanvasObject{buildPageSurface(scanView)}
						content.Refresh()
					}
					statusLabel.SetText("Ready")
				})
			}
			resultsView := buildResultsView(
				w,
				onBack,
				groups,
				sorted,
				dryRunCheck.Checked,
				initialSelection,
				appendOutput,
				duplicateDeleteModeFromLabel(deleteModeSelect.Selected),
				strings.TrimSpace(quarantinePathEntry.Text),
			)

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
					content.Objects = []fyne.CanvasObject{buildPageSurface(resultsView)}
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
		widget.NewFormItem("Remove duplicates by", deleteModeSelect),
		widget.NewFormItem("Safety backup folder", quarantinePathEntry),
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
	cleanupView, cleanupSettingsTabs := buildCleanupView(w, func(report devcleanup.RunReport) {
		healthState.LastCleanupAt = time.Now()
		_ = saveGUIHealthState(healthState)
		fyne.Do(func() {
			dupCountLabel.SetText(fmt.Sprintf("%s: %d", localize(healthState.Accessibility.Language, "last_dups"), healthState.LastDuplicateGroups))
			lastCleanupLabel.SetText(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + healthState.LastCleanupAt.Local().Format("2006-01-02 15:04"))
		})
		_ = report
		refreshHealthCard()
	})
	homeToDuplicateBtn := newHoverButton(localize(healthState.Accessibility.Language, "duplicate_tab"), func() {}, nil)
	homeToCleanupBtn := newHoverButton(localize(healthState.Accessibility.Language, "cleanup_tab"), func() {}, nil)
	homeToDuplicateBtn.Importance = widget.HighImportance
	homeToCleanupBtn.Importance = widget.HighImportance
	heroTitle := widget.NewLabel("Smarter storage cleanup")
	heroTitle.TextStyle = fyne.TextStyle{Bold: true}
	heroSubtitle := widget.NewLabel("Clean safely, remove duplicates, and keep your computer healthy.")
	heroSubtitle.Wrapping = fyne.TextWrapWord
	heroGlow := canvas.NewCircle(color.RGBA{R: 80, G: 120, B: 255, A: 80})
	heroGlow.Resize(fyne.NewSize(96, 96))
	heroIcon := widget.NewIcon(theme.ComputerIcon())
	heroLiftSpacer := canvas.NewRectangle(color.Transparent)
	heroLiftSpacer.SetMinSize(fyne.NewSize(1, 2))
	heroCardBg := canvas.NewRectangle(color.RGBA{R: 26, G: 30, B: 38, A: 255})
	heroCardBg.CornerRadius = 10
	startPulseAnimation(heroGlow)
	homeToDuplicateBtn.onHover = func(entered bool) {
		if entered {
			homeToDuplicateBtn.Importance = widget.WarningImportance
		} else {
			homeToDuplicateBtn.Importance = widget.HighImportance
		}
		homeToDuplicateBtn.Refresh()
	}
	homeToCleanupBtn.onHover = func(entered bool) {
		if entered {
			homeToCleanupBtn.Importance = widget.WarningImportance
		} else {
			homeToCleanupBtn.Importance = widget.HighImportance
		}
		homeToCleanupBtn.Refresh()
	}
	cacheTile := buildStatTile(localize(healthState.Accessibility.Language, "cache_size"), cacheSizeLabel, color.RGBA{R: 41, G: 98, B: 255, A: 255})
	tempTile := buildStatTile(localize(healthState.Accessibility.Language, "temp_size"), tempSizeLabel, color.RGBA{R: 0, G: 137, B: 123, A: 255})
	dupTile := buildStatTile(localize(healthState.Accessibility.Language, "last_dups"), dupCountLabel, color.RGBA{R: 156, G: 39, B: 176, A: 255})
	cleanupTile := buildStatTile(localize(healthState.Accessibility.Language, "last_cleanup"), lastCleanupLabel, color.RGBA{R: 255, G: 152, B: 0, A: 255})
	statTiles := []fyne.CanvasObject{cacheTile, tempTile, dupTile, cleanupTile}
	statsGrid := container.NewMax()
	healthTitleLabel := widget.NewLabel(localize(healthState.Accessibility.Language, "system_health"))
	healthHeader := container.NewBorder(
		nil,
		nil,
		healthTitleLabel,
		widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { refreshHealthCard() }),
		nil,
	)
	healthCard := container.NewVBox(
		healthHeader,
		statsGrid,
		dashboardCharts,
		healthStatusLabel,
	)
	heroSection := container.NewBorder(
		container.NewVBox(heroLiftSpacer),
		nil,
		nil,
		nil,
		container.NewStack(
			heroCardBg,
			container.NewPadded(container.NewBorder(
				nil,
				nil,
				container.NewStack(heroGlow, container.NewCenter(heroIcon)),
				nil,
				container.NewVBox(
					heroTitle,
					heroSubtitle,
					container.NewHBox(homeToDuplicateBtn, homeToCleanupBtn),
				),
			)),
		),
	)
	homeView = container.NewBorder(
		container.NewVBox(widget.NewLabel(fmt.Sprintf("Duplica Scan GUI %s", buildinfo.Version))),
		nil,
		nil,
		nil,
		container.NewVScroll(container.NewVBox(heroSection, healthCard)),
	)
	content = container.NewMax(buildPageSurface(homeView))

	var homeTabBtn *widget.Button
	var duplicateTabBtn *widget.Button
	var cleanupTabBtn *widget.Button
	var transitionSeq int64
	animateSwitch := func(target fyne.CanvasObject) {
		seq := atomic.AddInt64(&transitionSeq, 1)
		go func() {
			const steps = 8
			const frame = 18 * time.Millisecond
			for i := 0; i < steps; i++ {
				if atomic.LoadInt64(&transitionSeq) != seq {
					return
				}
				pad := float32((steps - i) * 3)
				fyne.Do(func() {
					spacer := canvas.NewRectangle(color.Transparent)
					spacer.SetMinSize(fyne.NewSize(1, pad))
					content.Objects = []fyne.CanvasObject{
						container.NewBorder(
							container.NewVBox(spacer),
							nil,
							nil,
							nil,
							buildPageSurface(target),
						),
					}
					content.Refresh()
				})
				time.Sleep(frame)
			}
			if atomic.LoadInt64(&transitionSeq) != seq {
				return
			}
			fyne.Do(func() {
				content.Objects = []fyne.CanvasObject{buildPageSurface(target)}
				content.Refresh()
			})
		}()
	}
	updateTab := func(tab string) {
		if tab == "cleanup" {
			animateSwitch(cleanupView)
			homeTabBtn.Importance = widget.MediumImportance
			duplicateTabBtn.Importance = widget.MediumImportance
			cleanupTabBtn.Importance = widget.HighImportance
		} else if tab == "duplicate" {
			animateSwitch(scanView)
			homeTabBtn.Importance = widget.MediumImportance
			duplicateTabBtn.Importance = widget.HighImportance
			cleanupTabBtn.Importance = widget.MediumImportance
		} else {
			animateSwitch(homeView)
			homeTabBtn.Importance = widget.HighImportance
			duplicateTabBtn.Importance = widget.MediumImportance
			cleanupTabBtn.Importance = widget.MediumImportance
		}
		homeTabBtn.Refresh()
		duplicateTabBtn.Refresh()
		cleanupTabBtn.Refresh()
	}
	homeTabBtn = widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
		updateTab("home")
	})
	duplicateTabBtn = widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		updateTab("duplicate")
	})
	cleanupTabBtn = widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		updateTab("cleanup")
	})
	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		openSettingsHub(duplicateSettingsTabs, cleanupSettingsTabs, updateTab, &healthState.Accessibility, func() {
			applyAccessibilityTheme(a, healthState.Accessibility)
			healthTitleLabel.SetText(localize(healthState.Accessibility.Language, "system_health"))
			homeToDuplicateBtn.SetText(localize(healthState.Accessibility.Language, "duplicate_tab"))
			homeToCleanupBtn.SetText(localize(healthState.Accessibility.Language, "cleanup_tab"))
			cacheSizeLabel.SetText(localize(healthState.Accessibility.Language, "cache_size") + ": ...")
			tempSizeLabel.SetText(localize(healthState.Accessibility.Language, "temp_size") + ": ...")
			dupCountLabel.SetText(fmt.Sprintf("%s: %d", localize(healthState.Accessibility.Language, "last_dups"), healthState.LastDuplicateGroups))
			if healthState.LastCleanupAt.IsZero() {
				lastCleanupLabel.SetText(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + localize(healthState.Accessibility.Language, "never"))
			} else {
				lastCleanupLabel.SetText(localize(healthState.Accessibility.Language, "last_cleanup") + ": " + healthState.LastCleanupAt.Local().Format("2006-01-02 15:04"))
			}
			_ = saveGUIHealthState(healthState)
			refreshHealthCard()
		})
	})
	settingsBtn.Importance = widget.MediumImportance
	navButtonSize := fyne.NewSize(52, 52)
	makeSquareNavItem := func(btn *widget.Button) fyne.CanvasObject {
		return container.NewGridWrap(navButtonSize, btn)
	}

	sidebarBg := canvas.NewRectangle(color.RGBA{R: 21, G: 24, B: 31, A: 255})
	sidebarBg.SetMinSize(fyne.NewSize(220, 10))
	sidebar := container.NewStack(
		sidebarBg,
		container.NewPadded(container.NewVBox(
			widget.NewLabel("Duplica Scan"),
			widget.NewSeparator(),
			makeSquareNavItem(homeTabBtn),
			makeSquareNavItem(duplicateTabBtn),
			makeSquareNavItem(cleanupTabBtn),
			widget.NewSeparator(),
			makeSquareNavItem(settingsBtn),
		)),
	)
	homeToDuplicateBtn.OnTapped = func() { updateTab("duplicate") }
	homeToCleanupBtn.OnTapped = func() { updateTab("cleanup") }
	applyResponsiveDashboard := func(width float32) {
		columns := 2
		if width < 1450 {
			columns = 1
			heroSubtitle.SetText("Clean safely, remove duplicates, and keep your computer healthy.")
		} else {
			heroSubtitle.SetText("Clean safely, remove duplicates, and keep your computer healthy.")
		}
		statsGrid.Objects = []fyne.CanvasObject{container.NewGridWithColumns(columns, statTiles...)}
		statsGrid.Refresh()
		sidebarWidth := float32(230)
		if width < 1400 {
			sidebarWidth = 205
		}
		sidebarBg.SetMinSize(fyne.NewSize(sidebarWidth, 10))
		sidebarBg.Refresh()
	}
	if strings.EqualFold(strings.TrimSpace(healthState.Accessibility.StartPage), "Cleanup") {
		updateTab("cleanup")
	} else if strings.EqualFold(strings.TrimSpace(healthState.Accessibility.StartPage), "Duplicate Files") {
		updateTab("duplicate")
	} else {
		updateTab("home")
	}
	if canvas := w.Canvas(); canvas != nil {
		canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.Key0, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
			updateTab("home")
		})
		canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.Key1, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
			updateTab("duplicate")
		})
		canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.Key2, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
			updateTab("cleanup")
		})
		canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
			settingsBtn.OnTapped()
		})
		canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
			refreshHealthCard()
		})
	}
	w.SetContent(container.NewBorder(nil, nil, sidebar, nil, content))
	applyResponsiveDashboard(w.Canvas().Size().Width)
	go func() {
		lastWidth := float32(0)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			width := w.Canvas().Size().Width
			if math.Abs(float64(width-lastWidth)) < 10 {
				continue
			}
			lastWidth = width
			fyne.Do(func() {
				applyResponsiveDashboard(width)
			})
		}
	}()
	refreshHealthCard()
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

func duplicateDeleteModeFromLabel(label string) cleanup.DeletionMode {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "move to safety backup":
		return cleanup.DeletionModeQuarantine
	default:
		return cleanup.DeletionModeDelete
	}
}

func openSettingsHub(
	duplicateSettings fyne.CanvasObject,
	cleanupSettings fyne.CanvasObject,
	updateTab func(string),
	prefs *accessibilityPrefs,
	onPrefsChanged func(),
) {
	hub := fyne.CurrentApp().NewWindow(localize(prefs.Language, "settings"))
	hub.Resize(fyne.NewSize(900, 620))

	landingSelect := widget.NewSelect([]string{"Home", "Duplicate Files", "Cleanup"}, nil)
	landingSelect.SetSelected(prefs.StartPage)
	themeSelect := widget.NewSelect([]string{"system", "dark", "light", "high-contrast-dark", "high-contrast-light"}, nil)
	themeSelect.SetSelected(prefs.ThemeMode)
	languageSelect := widget.NewSelect([]string{"en", "fa"}, nil)
	languageSelect.SetSelected(prefs.Language)
	textSizeSelect := widget.NewSelect([]string{"normal", "large"}, nil)
	textSizeSelect.SetSelected(prefs.TextScale)
	applyGeneralBtn := widget.NewButton(localize(prefs.Language, "save_changes"), func() {
		prefs.ThemeMode = strings.TrimSpace(themeSelect.Selected)
		prefs.Language = strings.TrimSpace(languageSelect.Selected)
		prefs.TextScale = strings.TrimSpace(textSizeSelect.Selected)
		prefs.StartPage = strings.TrimSpace(landingSelect.Selected)
		if landingSelect.Selected == "Cleanup" {
			updateTab("cleanup")
		} else if landingSelect.Selected == "Duplicate Files" {
			updateTab("duplicate")
		} else {
			updateTab("home")
		}
		if onPrefsChanged != nil {
			onPrefsChanged()
		}
	})
	generalForm := widget.NewForm(
		widget.NewFormItem(localize(prefs.Language, "home_page"), landingSelect),
		widget.NewFormItem(localize(prefs.Language, "theme"), themeSelect),
		widget.NewFormItem(localize(prefs.Language, "language"), languageSelect),
		widget.NewFormItem(localize(prefs.Language, "text_size"), textSizeSelect),
	)
	var rootTabs *container.AppTabs
	settingsSearch := widget.NewEntry()
	settingsSearch.SetPlaceHolder("Search settings (theme, language, duplicate, cleanup)")
	settingsSearchResult := widget.NewLabel("")
	jumpTo := func(target string) {
		switch target {
		case "duplicate":
			if rootTabs != nil {
				rootTabs.SelectIndex(1)
			}
		case "cleanup":
			if rootTabs != nil {
				rootTabs.SelectIndex(2)
			}
		default:
			if rootTabs != nil {
				rootTabs.SelectIndex(0)
			}
		}
	}
	settingsSearch.OnChanged = func(raw string) {
		q := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case q == "":
			settingsSearchResult.SetText("")
		case strings.Contains(q, "dup"):
			settingsSearchResult.SetText("Jump: Duplicate Files settings")
			jumpTo("duplicate")
		case strings.Contains(q, "clean"), strings.Contains(q, "risk"), strings.Contains(q, "quarantine"):
			settingsSearchResult.SetText("Jump: Cleanup settings")
			jumpTo("cleanup")
		case strings.Contains(q, "theme"), strings.Contains(q, "language"), strings.Contains(q, "text"), strings.Contains(q, "home"):
			settingsSearchResult.SetText("Jump: General settings")
			jumpTo("general")
		default:
			settingsSearchResult.SetText("No direct match. Try: theme, language, duplicate, cleanup")
		}
	}
	quickSettingsCards := container.NewGridWithColumns(
		3,
		buildFeatureCard("General", "Theme, language, text size, default page", theme.SettingsIcon(), func() { jumpTo("general") }),
		buildFeatureCard("Duplicate Files", "Matching mode, filters, deletion output", theme.SearchIcon(), func() { jumpTo("duplicate") }),
		buildFeatureCard("Cleanup", "Risk, scope, reports, safety backup", theme.DeleteIcon(), func() { jumpTo("cleanup") }),
	)
	generalSection := container.NewVBox(
		widget.NewLabel(localize(prefs.Language, "settings")),
		widget.NewLabel("Find and open settings quickly"),
		settingsSearch,
		settingsSearchResult,
		quickSettingsCards,
		widget.NewSeparator(),
		widget.NewLabel("Accessibility and localization"),
		generalForm,
		container.NewHBox(layout.NewSpacer(), applyGeneralBtn),
	)

	rootTabs = container.NewAppTabs(
		container.NewTabItem(localize(prefs.Language, "settings"), container.NewPadded(generalSection)),
		container.NewTabItem(localize(prefs.Language, "duplicate_tab"), duplicateSettings),
		container.NewTabItem(localize(prefs.Language, "cleanup_tab"), cleanupSettings),
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

type accessibilityPrefs struct {
	Language   string `json:"language"`
	TextScale  string `json:"text_scale"`
	ThemeMode  string `json:"theme_mode"`
	StartPage  string `json:"start_page"`
}

type scaledTheme struct {
	base  fyne.Theme
	scale float32
}

func (t scaledTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color { return t.base.Color(n, v) }
func (t scaledTheme) Font(s fyne.TextStyle) fyne.Resource                          { return t.base.Font(s) }
func (t scaledTheme) Icon(n fyne.ThemeIconName) fyne.Resource                      { return t.base.Icon(n) }
func (t scaledTheme) Size(n fyne.ThemeSizeName) float32                            { return t.base.Size(n) * t.scale }

type highContrastTheme struct {
	base fyne.Theme
	dark bool
}

func (t highContrastTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		if t.dark {
			return color.RGBA{R: 12, G: 12, B: 12, A: 255}
		}
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNameForeground:
		if t.dark {
			return color.RGBA{R: 255, G: 255, B: 255, A: 255}
		}
		return color.RGBA{R: 0, G: 0, B: 0, A: 255}
	case theme.ColorNameButton:
		if t.dark {
			return color.RGBA{R: 38, G: 38, B: 38, A: 255}
		}
		return color.RGBA{R: 230, G: 230, B: 230, A: 255}
	case theme.ColorNamePrimary:
		return color.RGBA{R: 255, G: 193, B: 7, A: 255}
	default:
		variant := theme.VariantDark
		if !t.dark {
			variant = theme.VariantLight
		}
		return t.base.Color(n, variant)
	}
}

func (t highContrastTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.base.Font(s) }
func (t highContrastTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }
func (t highContrastTheme) Size(n fyne.ThemeSizeName) float32       { return t.base.Size(n) }

type hoverButton struct {
	widget.Button
	onHover func(entered bool)
}

func newHoverButton(label string, tapped func(), onHover func(bool)) *hoverButton {
	b := &hoverButton{onHover: onHover}
	b.ExtendBaseWidget(b)
	b.SetText(label)
	b.OnTapped = tapped
	return b
}

func (b *hoverButton) MouseIn(*desktop.MouseEvent) {
	if b.onHover != nil {
		b.onHover(true)
	}
}

func (b *hoverButton) MouseMoved(*desktop.MouseEvent) {}

func (b *hoverButton) MouseOut() {
	if b.onHover != nil {
		b.onHover(false)
	}
}

func localize(lang, key string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	texts := map[string]map[string]string{
		"en": {
			"home_tab":      "Home",
			"duplicate_tab": "Duplicate Files",
			"cleanup_tab":   "Cleanup",
			"system_health": "System Health",
			"cache_size":    "Cache size",
			"temp_size":     "Temp size",
			"last_dups":     "Last duplicate groups",
			"last_cleanup":  "Last cleanup",
			"never":         "never",
			"settings":      "Settings",
			"save_changes":  "Save Changes",
			"theme":         "Theme",
			"language":      "Language",
			"text_size":     "Text size",
			"home_page":     "Open this page first",
		},
		"fa": {
			"home_tab":      "خانه",
			"duplicate_tab": "فایل‌های تکراری",
			"cleanup_tab":   "پاکسازی",
			"system_health": "وضعیت سیستم",
			"cache_size":    "حجم کش",
			"temp_size":     "حجم فایل‌های موقت",
			"last_dups":     "آخرین تعداد گروه‌های تکراری",
			"last_cleanup":  "آخرین پاکسازی",
			"never":         "هرگز",
			"settings":      "تنظیمات",
			"save_changes":  "ذخیره تغییرات",
			"theme":         "تم",
			"language":      "زبان",
			"text_size":     "اندازه متن",
			"home_page":     "صفحه شروع پیش‌فرض",
		},
	}
	if _, ok := texts[lang]; !ok {
		lang = "en"
	}
	if v, ok := texts[lang][key]; ok {
		return v
	}
	return key
}

func applyAccessibilityTheme(a fyne.App, prefs accessibilityPrefs) {
	mode := strings.ToLower(strings.TrimSpace(prefs.ThemeMode))
	scale := float32(1.0)
	if strings.EqualFold(strings.TrimSpace(prefs.TextScale), "large") {
		scale = 1.25
	}
	var base fyne.Theme
	switch mode {
	case "dark":
		base = theme.DarkTheme()
	case "light":
		base = theme.LightTheme()
	case "high-contrast-dark":
		base = highContrastTheme{base: theme.DarkTheme(), dark: true}
	case "high-contrast-light":
		base = highContrastTheme{base: theme.LightTheme(), dark: false}
	default:
		base = theme.DefaultTheme()
	}
	if scale != 1 {
		base = scaledTheme{base: base, scale: scale}
	}
	a.Settings().SetTheme(base)
}

type guiHealthState struct {
	LastCleanupAt       time.Time `json:"last_cleanup_at"`
	LastDuplicateAt     time.Time `json:"last_duplicate_at"`
	LastDuplicateGroups int       `json:"last_duplicate_groups"`
	Accessibility       accessibilityPrefs `json:"accessibility"`
}

func guiHealthStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".duplica-scan", "gui-health.json"), nil
}

func loadGUIHealthState() (guiHealthState, error) {
	path, err := guiHealthStatePath()
	if err != nil {
		return guiHealthState{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return guiHealthState{}, nil
		}
		return guiHealthState{}, err
	}
	var state guiHealthState
	if err := json.Unmarshal(raw, &state); err != nil {
		return guiHealthState{}, err
	}
	return state, nil
}

func saveGUIHealthState(state guiHealthState) error {
	path, err := guiHealthStatePath()
	if err != nil {
		return err
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

func estimateSystemHealth() (int64, int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0, err
	}
	cachePaths := []string{
		filepath.Join(home, ".cache"),
		filepath.Join(home, ".npm"),
		filepath.Join(home, ".nuget"),
		filepath.Join(home, ".gradle"),
		filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Cache"),
		filepath.Join(home, "AppData", "Local", "Microsoft", "Edge", "User Data", "Default", "Cache"),
	}
	tempPaths := []string{
		os.TempDir(),
		filepath.Join(home, "AppData", "Local", "Temp"),
	}
	cacheBytes, cacheErr := sumExistingDirSizes(cachePaths)
	tempBytes, tempErr := sumExistingDirSizes(tempPaths)
	if cacheErr != nil {
		return cacheBytes, tempBytes, cacheErr
	}
	return cacheBytes, tempBytes, tempErr
}

func sumExistingDirSizes(paths []string) (int64, error) {
	seen := make(map[string]struct{}, len(paths))
	var total int64
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		info, err := os.Stat(clean)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.Walk(clean, func(_ string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info == nil || info.IsDir() {
				return nil
			}
			total += info.Size()
			return nil
		})
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func startPulseAnimation(target *canvas.Circle) {
	if target == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		start := time.Now()
		for t := range ticker.C {
			phase := float64(t.Sub(start).Milliseconds()%2200) / 2200.0
			// Smooth breathing pulse between ~40 and ~110 alpha.
			alpha := uint8(40 + 70*(0.5+0.5*math.Sin(phase*2*math.Pi)))
			fyne.Do(func() {
				target.FillColor = color.RGBA{R: 80, G: 120, B: 255, A: alpha}
				target.Refresh()
			})
		}
	}()
}

func buildPageSurface(page fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.RGBA{R: 16, G: 19, B: 25, A: 255})
	return container.NewStack(
		bg,
		container.NewPadded(page),
	)
}

func buildStatTile(title string, value *widget.Label, accent color.Color) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.RGBA{R: 31, G: 36, B: 46, A: 255})
	bg.CornerRadius = 8
	bar := canvas.NewRectangle(accent)
	bar.SetMinSize(fyne.NewSize(4, 62))
	titleLabel := widget.NewLabel(title)
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}
	titleLabel.Alignment = fyne.TextAlignLeading
	value.Alignment = fyne.TextAlignLeading
	body := container.NewBorder(
		nil,
		nil,
		container.NewGridWrap(fyne.NewSize(8, 70), container.NewCenter(bar)),
		nil,
		container.NewPadded(container.NewGridWrap(
			fyne.NewSize(240, 70),
			container.NewCenter(container.NewVBox(titleLabel, value)),
		)),
	)
	return container.NewGridWrap(fyne.NewSize(300, 82), container.NewStack(bg, body))
}

func buildDashboardMiniCharts(cacheBytes int64, tempBytes int64, duplicateGroups int, lastCleanupAt time.Time) fyne.CanvasObject {
	type metric struct {
		name  string
		value float64
		label string
		color color.Color
	}
	cleanupRecencyHours := float64(0)
	if !lastCleanupAt.IsZero() {
		cleanupRecencyHours = time.Since(lastCleanupAt).Hours()
		if cleanupRecencyHours < 0 {
			cleanupRecencyHours = 0
		}
	}
	metrics := []metric{
		{name: "Cache", value: float64(cacheBytes), label: formatBytes(cacheBytes), color: color.RGBA{R: 41, G: 98, B: 255, A: 255}},
		{name: "Temp", value: float64(tempBytes), label: formatBytes(tempBytes), color: color.RGBA{R: 0, G: 137, B: 123, A: 255}},
		{name: "Duplicates", value: float64(duplicateGroups), label: fmt.Sprintf("%d groups", duplicateGroups), color: color.RGBA{R: 156, G: 39, B: 176, A: 255}},
		{name: "Cleanup age", value: cleanupRecencyHours, label: fmt.Sprintf("%.0f h", cleanupRecencyHours), color: color.RGBA{R: 255, G: 152, B: 0, A: 255}},
	}
	maxValue := float64(1)
	for _, m := range metrics {
		if m.value > maxValue {
			maxValue = m.value
		}
	}
	rows := make([]fyne.CanvasObject, 0, len(metrics)+1)
	rows = append(rows, widget.NewLabel("Dashboard charts"))
	for _, m := range metrics {
		ratio := float32(m.value / maxValue)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		width := float32(260)
		fill := ratio * width
		if fill < 3 && m.value > 0 {
			fill = 3
		}
		bg := canvas.NewRectangle(color.RGBA{R: 52, G: 58, B: 68, A: 255})
		bg.SetMinSize(fyne.NewSize(width, 14))
		fg := canvas.NewRectangle(m.color)
		bar := container.NewStack(bg, container.NewGridWrap(fyne.NewSize(fill, 14), fg))
		nameLabel := widget.NewLabel(m.name)
		nameLabel.Alignment = fyne.TextAlignLeading
		valueLabel := widget.NewLabel(m.label)
		valueLabel.Alignment = fyne.TextAlignLeading
		rows = append(rows, container.NewBorder(
			nil, nil,
			container.NewGridWrap(fyne.NewSize(100, 24), container.NewCenter(nameLabel)),
			container.NewGridWrap(fyne.NewSize(100, 24), container.NewCenter(valueLabel)),
			container.NewGridWrap(fyne.NewSize(width, 24), container.NewCenter(bar)),
		))
	}
	return container.NewVBox(rows...)
}

func buildFeatureCard(title string, subtitle string, icon fyne.Resource, onOpen func()) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.RGBA{R: 29, G: 34, B: 43, A: 255})
	bg.CornerRadius = 10
	titleLabel := widget.NewLabel(title)
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}
	subLabel := widget.NewLabel(subtitle)
	subLabel.Wrapping = fyne.TextWrapWord
	openBtn := widget.NewButtonWithIcon("Open", icon, func() {
		if onOpen != nil {
			onOpen()
		}
	})
	openBtn.Importance = widget.HighImportance
	content := container.NewPadded(container.NewVBox(
		titleLabel,
		subLabel,
		container.NewHBox(layout.NewSpacer(), openBtn),
	))
	return container.NewStack(bg, content)
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

func buildCleanupView(parent fyne.Window, onCleanupFinished func(devcleanup.RunReport)) (fyne.CanvasObject, fyne.CanvasObject) {
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
	scheduleModeSelect := widget.NewSelect([]string{"off", "weekly", "monthly"}, nil)
	scheduleModeSelect.SetSelected("off")
	scheduledProfileSelect := widget.NewSelect([]string{"quick-safe"}, nil)
	scheduledProfileSelect.SetSelected("quick-safe")
	scheduleStateEntry := widget.NewEntry()
	scheduleStateEntry.SetText(".duplica-scan/scheduler-state.json")
	scheduledReportDirEntry := widget.NewEntry()
	scheduledReportDirEntry.SetText(".duplica-scan/reports")
	cleanupDeleteModeSelect := widget.NewSelect([]string{"Delete permanently", "Move to safety backup"}, nil)
	cleanupDeleteModeSelect.SetSelected("Delete permanently")
	cleanupQuarantineEntry := widget.NewEntry()
	cleanupQuarantineEntry.SetPlaceHolder("Optional safety backup folder")
	undoDaysEntry := widget.NewEntry()
	undoDaysEntry.SetText("7")
	restoreUndoBtn := widget.NewButton("Restore from safety backup", func() {
		days, err := parseInt(undoDaysEntry.Text, 7)
		if err != nil || days <= 0 {
			dialog.ShowInformation("Invalid undo window", "Undo window days must be a positive number.", parent)
			return
		}
		openQuarantineRestoreWindow(parent, strings.TrimSpace(cleanupQuarantineEntry.Text), days)
	})
	pruneUndoBtn := widget.NewButton("Delete expired backups", func() {
		days, err := parseInt(undoDaysEntry.Text, 7)
		if err != nil || days <= 0 {
			dialog.ShowInformation("Invalid undo window", "Undo window days must be a positive number.", parent)
			return
		}
		removed, pruneErr := cleanup.PruneExpiredQuarantine(strings.TrimSpace(cleanupQuarantineEntry.Text), days)
		if pruneErr != nil {
			dialog.ShowError(pruneErr, parent)
			return
		}
		dialog.ShowInformation("Undo window cleanup", fmt.Sprintf("Removed %d expired backup items.", removed), parent)
	})
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

	helpText := widget.NewMultiLineEntry()
	helpText.Disable()
	helpText.Wrapping = fyne.TextWrapWord
	helpText.SetMinRowsVisible(9)
	refreshHelpText := func() {
		helpText.SetText(buildCleanupExplainerText(riskSelect.Selected, includeCategories.Text))
	}
	riskSelect.OnChanged = func(string) { refreshHelpText() }
	includeCategories.OnChanged = func(string) { refreshHelpText() }
	refreshHelpText()

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
			DeleteMode:          string(duplicateDeleteModeFromLabel(cleanupDeleteModeSelect.Selected)),
			QuarantineDir:       strings.TrimSpace(cleanupQuarantineEntry.Text),
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
			var report devcleanup.RunReport
			var runErr error
			usedSchedule := false
			if scheduleModeSelect.Selected != "off" {
				appendOutput("Checking schedule window before cleanup...")
				report, usedSchedule, runErr = devcleanup.RunScheduledCleanup(
					context.Background(),
					engine,
					devcleanup.BuiltinSafeProfile(strings.TrimSpace(strings.ToLower(scheduledProfileSelect.Selected))),
					devcleanup.ScheduleKind(strings.TrimSpace(strings.ToLower(scheduleModeSelect.Selected))),
					strings.TrimSpace(scheduleStateEntry.Text),
					strings.TrimSpace(scheduledReportDirEntry.Text),
				)
				if runErr == nil && !usedSchedule {
					fyne.Do(func() {
						runBtn.Enable()
						status.SetText("Skipped (already run in this period)")
					})
					appendOutput("Scheduled cleanup skipped (already run in the current period).")
					return
				}
			} else {
				report, runErr = engine.Run(context.Background(), cfg)
			}
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
			if usedSchedule {
				appendOutput("Cleanup run mode: scheduled")
			}

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
				if onCleanupFinished != nil {
					onCleanupFinished(report)
				}
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
		widget.NewFormItem("Auto schedule", scheduleModeSelect),
		widget.NewFormItem("Schedule profile", scheduledProfileSelect),
		widget.NewFormItem("Schedule state file", scheduleStateEntry),
		widget.NewFormItem("Scheduled reports folder", scheduledReportDirEntry),
		widget.NewFormItem("Delete mode", cleanupDeleteModeSelect),
		widget.NewFormItem("Safety backup folder", cleanupQuarantineEntry),
		widget.NewFormItem("Undo window days", undoDaysEntry),
		widget.NewFormItem("Restore backups", restoreUndoBtn),
		widget.NewFormItem("Prune expired backups", pruneUndoBtn),
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
			widget.NewLabel("What this does (help)"),
			helpText,
		),
		nil,
		nil,
		nil,
		container.NewVScroll(output),
	)
	cleanupRoot = container.NewMax(mainView)
	return cleanupRoot, cleanupSettingsTabs
}

func buildCleanupExplainerText(risk string, includeCategoriesRaw string) string {
	risk = strings.ToLower(strings.TrimSpace(risk))
	riskGuide := map[string]string{
		"safe":       "Safe: removes temporary/cache files that are usually recreated automatically. Lowest chance of side effects.",
		"moderate":   "Moderate: removes larger app/tool caches and logs. Usually safe, but some apps may need to rebuild cache.",
		"aggressive": "Aggressive: can remove build artifacts or heavy data that may take time to rebuild. Review before running.",
	}
	typeGuide := map[string]string{
		"os-temp":         "OS Temp: temporary system/user files that apps can recreate.",
		"package-manager": "Package Manager: npm/pip/nuget/gradle/cargo caches; frees space but may slow next install briefly.",
		"browser":         "Browser: Chrome/Edge/Firefox cache data; safe for space, may sign out some sessions in rare cases.",
		"logs":            "Logs: app crash and log files; useful for troubleshooting but safe to remove when not needed.",
		"build-artifact":  "Build Artifact: bin/obj/dist/target outputs; safe if you can rebuild projects.",
		"ide":             "IDE: VS Code/JetBrains/Visual Studio caches; may reset some workspace state.",
		"container":       "Container: Docker-related cleanup; can be high impact depending on images/volumes.",
		"gaming":          "Gaming: launcher/shader caches; usually safe and regenerated.",
		"android":         "Android: emulator/build caches; can free a lot but may trigger redownload/rebuild.",
		"flutter":         "Flutter/Dart: tool and package caches; safe but next build can be slower.",
		"ios":             "iOS/macOS dev caches: simulator/tool data; review before aggressive cleanup.",
	}

	selected := parseNames(includeCategoriesRaw)
	lines := []string{
		"Selected risk level:",
		"- " + riskGuide[risk],
		"",
		"Cleanup types:",
	}
	if len(selected) == 0 {
		keys := []string{"os-temp", "package-manager", "browser", "logs", "build-artifact", "ide", "container", "gaming", "android", "flutter", "ios"}
		for _, key := range keys {
			lines = append(lines, "- "+typeGuide[key])
		}
	} else {
		keys := make([]string, 0, len(selected))
		for key := range selected {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if text, ok := typeGuide[key]; ok {
				lines = append(lines, "- "+text)
			} else {
				lines = append(lines, "- "+key+": custom category selected.")
			}
		}
	}
	lines = append(lines, "", "Tip: Use \"Move to safety backup\" in Output settings for an undo option.")
	return strings.Join(lines, "\n")
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
	recommendations := buildCleanupRecommendations(report)
	recommendationItems := make([]fyne.CanvasObject, 0, len(recommendations)+1)
	recommendationItems = append(recommendationItems, widget.NewLabel("Smart recommendations"))
	for _, rec := range recommendations {
		recLabel := widget.NewLabel("• " + rec)
		recLabel.Wrapping = fyne.TextWrapWord
		recLabel.Alignment = fyne.TextAlignLeading
		recommendationItems = append(recommendationItems, recLabel)
	}
	recommendationBox := container.NewVBox(recommendationItems...)

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
			recommendationBox,
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

func buildCleanupRecommendations(report devcleanup.RunReport) []string {
	const bigThreshold = int64(1 << 30) // 1 GB
	const mediumThreshold = int64(512 << 20)

	recs := make([]string, 0, 6)
	var topBrowserName string
	var topBrowserBytes int64
	var topSafeName string
	var topSafeBytes int64
	aggressiveCount := 0
	errorCount := 0

	for _, result := range report.Results {
		if result.Error != "" {
			errorCount++
		}
		estimated := result.EstimatedBytes
		if estimated <= 0 {
			continue
		}
		risk := strings.ToLower(strings.TrimSpace(result.Risk))
		category := strings.ToLower(strings.TrimSpace(result.Category))

		if category == "browser" && estimated > topBrowserBytes {
			topBrowserBytes = estimated
			topBrowserName = result.Name
		}
		if (risk == "safe" || risk == "moderate") && estimated > topSafeBytes {
			topSafeBytes = estimated
			topSafeName = result.Name
		}
		if risk == "aggressive" && estimated >= mediumThreshold {
			aggressiveCount++
			if strings.Contains(strings.ToLower(result.Name), "docker") {
				recs = append(recs, fmt.Sprintf("%s may free %s, but it is aggressive. Use safety backup mode if unsure.", result.Name, formatBytes(estimated)))
			}
		}
	}

	if topBrowserName != "" && topBrowserBytes >= mediumThreshold {
		verdict := "worth cleaning"
		if topBrowserBytes >= bigThreshold {
			verdict = "safe to clean for quick space recovery"
		}
		recs = append(recs, fmt.Sprintf("%s is %s and is generally %s.", topBrowserName, formatBytes(topBrowserBytes), verdict))
	}
	if topSafeName != "" && topSafeBytes >= bigThreshold {
		recs = append(recs, fmt.Sprintf("%s is a large low-risk target (%s).", topSafeName, formatBytes(topSafeBytes)))
	}
	if aggressiveCount > 0 {
		recs = append(recs, fmt.Sprintf("%d aggressive cleanup target(s) detected. Review details before running permanent delete.", aggressiveCount))
	}
	if errorCount > 0 {
		recs = append(recs, fmt.Sprintf("%d task(s) had errors. Close related apps and retry, or use safety backup mode.", errorCount))
	}
	if len(recs) == 0 {
		recs = append(recs, "No high-impact recommendations found. Current cleanup profile already looks conservative.")
	}
	if len(recs) > 5 {
		recs = recs[:5]
	}
	return recs
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

	compareCategorySection := container.NewVBox(widget.NewLabel("Before vs after by type"))
	compareCategoryKeys := make([]string, 0, len(report.PlannedByCategory))
	for category := range report.PlannedByCategory {
		compareCategoryKeys = append(compareCategoryKeys, category)
	}
	sort.Strings(compareCategoryKeys)
	if len(compareCategoryKeys) == 0 {
		compareCategorySection.Add(widget.NewLabel("No type-level size comparison data available for this run."))
	} else {
		maxCompareCategory := int64(1)
		for _, category := range compareCategoryKeys {
			before := report.PlannedByCategory[category]
			after := before - report.FreedByCategory[category]
			if after < 0 {
				after = 0
			}
			if before > maxCompareCategory {
				maxCompareCategory = before
			}
			if after > maxCompareCategory {
				maxCompareCategory = after
			}
		}
		for _, category := range compareCategoryKeys {
			before := report.PlannedByCategory[category]
			after := before - report.FreedByCategory[category]
			if after < 0 {
				after = 0
			}
			compareCategorySection.Add(buildBeforeAfterBars(category, before, after, maxCompareCategory))
		}
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

	compareVolumeSection := container.NewVBox(widget.NewLabel("Before vs after by drive"))
	compareVolumeKeys := make([]string, 0, len(report.PlannedByVolume))
	for volume := range report.PlannedByVolume {
		compareVolumeKeys = append(compareVolumeKeys, volume)
	}
	sort.Strings(compareVolumeKeys)
	if len(compareVolumeKeys) == 0 {
		compareVolumeSection.Add(widget.NewLabel("No drive-level size comparison data available for this run."))
	} else {
		maxCompareVolume := int64(1)
		for _, volume := range compareVolumeKeys {
			before := report.PlannedByVolume[volume]
			after := before - report.FreedByVolume[volume]
			if after < 0 {
				after = 0
			}
			if before > maxCompareVolume {
				maxCompareVolume = before
			}
			if after > maxCompareVolume {
				maxCompareVolume = after
			}
		}
		for _, volume := range compareVolumeKeys {
			before := report.PlannedByVolume[volume]
			after := before - report.FreedByVolume[volume]
			if after < 0 {
				after = 0
			}
			compareVolumeSection.Add(buildBeforeAfterBars(volume, before, after, maxCompareVolume))
		}
	}

	return container.NewVScroll(container.NewVBox(
		statusSection,
		widget.NewSeparator(),
		categorySection,
		widget.NewSeparator(),
		compareCategorySection,
		widget.NewSeparator(),
		volumeSection,
		widget.NewSeparator(),
		compareVolumeSection,
	))
}

func buildBeforeAfterBars(name string, before int64, after int64, max int64) fyne.CanvasObject {
	beforeLine := buildHorizontalBar("Before", before, max, color.RGBA{R: 120, G: 144, B: 156, A: 255}, formatBytes(before))
	afterLine := buildHorizontalBar("After", after, max, color.RGBA{R: 76, G: 175, B: 80, A: 255}, formatBytes(after))
	title := widget.NewLabel(name)
	title.Alignment = fyne.TextAlignLeading
	return container.NewVBox(
		title,
		container.NewPadded(beforeLine),
		container.NewPadded(afterLine),
	)
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

func openQuarantineRestoreWindow(parent fyne.Window, quarantineDir string, days int) {
	entries, err := cleanup.ListQuarantineEntries(quarantineDir, days)
	if err != nil {
		dialog.ShowError(err, parent)
		return
	}
	if len(entries) == 0 {
		dialog.ShowInformation("No backups found", fmt.Sprintf("No backup items found in the last %d days.", days), parent)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})

	restoreWindow := fyne.CurrentApp().NewWindow("Restore from Safety Backup")
	restoreWindow.Resize(fyne.NewSize(900, 500))

	selected := 0
	list := widget.NewList(
		func() int { return len(entries) },
		func() fyne.CanvasObject { return widget.NewLabel("item") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			e := entries[id]
			label := obj.(*widget.Label)
			label.SetText(fmt.Sprintf("%s | %s", e.CreatedAt.Format("2006-01-02 15:04"), e.OriginalPath))
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		selected = id
	}

	detail := widget.NewMultiLineEntry()
	detail.Disable()
	updateDetail := func() {
		e := entries[selected]
		detail.SetText(
			"Created: " + e.CreatedAt.Format(time.RFC1123) + "\n" +
				"Source: " + e.Source + "\n" +
				"Original path: " + e.OriginalPath + "\n" +
				"Backup path: " + e.BackupPath,
		)
	}
	updateDetail()
	list.Select(0)

	restoreBtn := widget.NewButton("Restore selected item", func() {
		e := entries[selected]
		if err := cleanup.RestoreQuarantineEntry(e); err != nil {
			dialog.ShowError(err, restoreWindow)
			return
		}
		dialog.ShowInformation("Restored", "Item restored successfully.", restoreWindow)
		entries = append(entries[:selected], entries[selected+1:]...)
		if len(entries) == 0 {
			restoreWindow.Close()
			return
		}
		if selected >= len(entries) {
			selected = len(entries) - 1
		}
		list.Refresh()
		list.Select(selected)
		updateDetail()
	})

	list.OnSelected = func(id widget.ListItemID) {
		selected = id
		updateDetail()
	}

	restoreWindow.SetContent(container.NewBorder(
		widget.NewLabel(fmt.Sprintf("Backups available in last %d days", days)),
		container.NewHBox(layout.NewSpacer(), restoreBtn),
		container.NewVScroll(list),
		nil,
		container.NewVScroll(detail),
	))
	restoreWindow.Show()
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
	modifiedAt   time.Time
	groupNum     int
	fileNum      int
	overflowNote string // non-empty => informational row (no checkbox)
}

const (
	keepRuleNewest         = "Keep newest"
	keepRuleOldest         = "Keep oldest"
	keepRuleLargest        = "Keep largest"
	keepRuleFolderPriority = "Keep by folder priority"
)

func chooseKeeper(files []model.FileMeta, rule string, folderPriorities []string) model.FileMeta {
	if len(files) == 0 {
		return model.FileMeta{}
	}
	best := files[0]
	bestScore := folderPriorityScore(best.Path, folderPriorities)
	for i := 1; i < len(files); i++ {
		current := files[i]
		switch rule {
		case keepRuleOldest:
			if current.ModifiedAt.Before(best.ModifiedAt) || (current.ModifiedAt.Equal(best.ModifiedAt) && current.Path < best.Path) {
				best = current
			}
		case keepRuleLargest:
			if current.Size > best.Size || (current.Size == best.Size && current.Path < best.Path) {
				best = current
			}
		case keepRuleFolderPriority:
			score := folderPriorityScore(current.Path, folderPriorities)
			if score < bestScore || (score == bestScore && (current.ModifiedAt.After(best.ModifiedAt) || (current.ModifiedAt.Equal(best.ModifiedAt) && current.Path < best.Path))) {
				best = current
				bestScore = score
			}
		default:
			if current.ModifiedAt.After(best.ModifiedAt) || (current.ModifiedAt.Equal(best.ModifiedAt) && current.Path < best.Path) {
				best = current
			}
		}
	}
	return best
}

func folderPriorityScore(path string, priorities []string) int {
	cleanPath := strings.ToLower(filepath.Clean(path))
	for i, root := range priorities {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleanRoot := strings.ToLower(filepath.Clean(root))
		if strings.HasPrefix(cleanPath, cleanRoot) {
			return i
		}
	}
	return len(priorities) + 1000
}

func applyKeepRule(groups []duplicates.Group, rule string, folderPriorities []string) []string {
	paths := make([]string, 0, 256)
	for _, group := range groups {
		if len(group.Files) < 2 {
			continue
		}
		keeper := chooseKeeper(group.Files, rule, folderPriorities)
		for _, file := range group.Files {
			if file.Path == keeper.Path {
				continue
			}
			paths = append(paths, file.Path)
		}
	}
	return paths
}

func buildResultsView(
	parent fyne.Window,
	onBack func(),
	originalGroups []duplicates.Group,
	sortedGroups []duplicates.Group,
	dryRun bool,
	initialSelection map[string]struct{},
	appendOutput func(string),
	deleteMode cleanup.DeletionMode,
	quarantineDir string,
) fyne.CanvasObject {
	totalGroupCount := len(sortedGroups)

	selected := make(map[string]struct{}, 512)
	for p := range initialSelection {
		selected[p] = struct{}{}
	}
	var pageRows []resultsTableRow
	groupFilesByNum := make(map[int][]model.FileMeta, len(sortedGroups))
	for i, group := range sortedGroups {
		groupFilesByNum[i+1] = group.Files
	}

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
	selectedGroupNum := 0
	selectedPath := ""
	var refreshCompare func()

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
					path:       file.Path,
					name:       file.Name,
					size:       file.Size,
					modifiedAt: file.ModifiedAt,
					groupNum:   gnum,
					fileNum:    fi + 1,
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
	resultsTable.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(pageRows) {
			return
		}
		row := pageRows[id.Row]
		if row.overflowNote != "" {
			return
		}
		selectedPath = row.path
		selectedGroupNum = row.groupNum
		refreshCompare()
	}

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
	keepRuleSelect := widget.NewSelect([]string{keepRuleNewest, keepRuleOldest, keepRuleLargest, keepRuleFolderPriority}, nil)
	keepRuleSelect.SetSelected(keepRuleNewest)
	folderPriorityEntry := widget.NewEntry()
	folderPriorityEntry.SetPlaceHolder("Folder priority list (comma-separated), e.g. D:/Work,D:/Archive")
	applyRuleBtn := widget.NewButton("Apply Keep Rule", func() {
		priorities := []string{}
		if keepRuleSelect.Selected == keepRuleFolderPriority {
			for _, part := range strings.Split(folderPriorityEntry.Text, ",") {
				p := strings.TrimSpace(part)
				if p != "" {
					priorities = append(priorities, p)
				}
			}
		}
		setSelection(applyKeepRule(originalGroups, keepRuleSelect.Selected, priorities))
	})

	selectedFileDetails := widget.NewMultiLineEntry()
	selectedFileDetails.Disable()
	keeperFileDetails := widget.NewMultiLineEntry()
	keeperFileDetails.Disable()
	refreshCompare = func() {
		if selectedGroupNum == 0 || selectedPath == "" {
			selectedFileDetails.SetText("Select a file row to compare.")
			keeperFileDetails.SetText("Rule-based keep candidate will appear here.")
			return
		}
		files := groupFilesByNum[selectedGroupNum]
		if len(files) == 0 {
			selectedFileDetails.SetText("No file data available for this group.")
			keeperFileDetails.SetText("No keep candidate available.")
			return
		}
		var current model.FileMeta
		found := false
		for _, f := range files {
			if f.Path == selectedPath {
				current = f
				found = true
				break
			}
		}
		if !found {
			selectedFileDetails.SetText("Selected file is not in current group data.")
			keeperFileDetails.SetText("No keep candidate available.")
			return
		}
		priorities := []string{}
		if keepRuleSelect.Selected == keepRuleFolderPriority {
			for _, part := range strings.Split(folderPriorityEntry.Text, ",") {
				p := strings.TrimSpace(part)
				if p != "" {
					priorities = append(priorities, p)
				}
			}
		}
		keeper := chooseKeeper(files, keepRuleSelect.Selected, priorities)
		selectedFileDetails.SetText(
			fmt.Sprintf(
				"Selected file\n\nName: %s\nSize: %s\nModified: %s\nPath: %s",
				current.Name,
				formatBytes(current.Size),
				current.ModifiedAt.Local().Format("2006-01-02 15:04:05"),
				current.Path,
			),
		)
		keeperFileDetails.SetText(
			fmt.Sprintf(
				"Keep candidate (%s)\n\nName: %s\nSize: %s\nModified: %s\nPath: %s",
				keepRuleSelect.Selected,
				keeper.Name,
				formatBytes(keeper.Size),
				keeper.ModifiedAt.Local().Format("2006-01-02 15:04:05"),
				keeper.Path,
			),
		)
	}
	keepRuleSelect.OnChanged = func(string) { refreshCompare() }
	folderPriorityEntry.OnChanged = func(string) { refreshCompare() }
	refreshCompare()

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
					for i := 0; i < n; i++ {
						idx := i + 1
						fyne.Do(func() {
							if n > 0 {
								prog.SetValue(float64(idx) / float64(n))
								status.SetText(fmt.Sprintf("Processing %d of %d (%.0f%%)…", idx, n, 100*float64(idx)/float64(n)))
							}
						})
					}
					results := cleanup.DeleteFilesWithOptions(paths, cleanup.DeleteOptions{
						DryRun:        dryRun,
						Mode:          deleteMode,
						QuarantineDir: quarantineDir,
					})
					failures := 0
					for _, result := range results {
						if result.Err != nil {
							failures++
							appendOutput(fmt.Sprintf("Failed: %s (%v)", result.Path, result.Err))
							continue
						}
						if result.BackupPath != "" {
							appendOutput(fmt.Sprintf("Moved to safety backup: %s -> %s", result.Path, result.BackupPath))
						}
					}
					fyne.Do(func() {
						dlg.Hide()
						appendOutput(fmt.Sprintf("Result action completed. Success: %d, Failed: %d", len(results)-failures, failures))
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
		selectAllBtn, clearBtn, keepNewestBtn, keepOldestBtn, keepRuleSelect, applyRuleBtn,
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
		folderPriorityEntry,
	)
	comparePane := container.NewVBox(
		widget.NewLabel("Side-by-side compare"),
		container.NewGridWithColumns(
			2,
			container.NewBorder(widget.NewLabel("Selected"), nil, nil, nil, selectedFileDetails),
			container.NewBorder(widget.NewLabel("Keep by rule"), nil, nil, nil, keeperFileDetails),
		),
	)

	out := container.NewBorder(
		container.NewPadded(top),
		container.NewPadded(bottomStack),
		nil,
		container.NewPadded(comparePane),
		resultsTable,
	)
	rebuildPage()
	return out
}
