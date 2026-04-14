package report

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cleanpulse/src/internal/duplicates"
)

const (
	FormatCSV  = "csv"
	FormatJSON = "json"
)

// Export writes duplicate groups to disk as csv or json.
func Export(groups []duplicates.Group, format string, outputPath string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return nil
	}
	if outputPath == "" {
		return errors.New("output path is required when export format is set")
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	switch format {
	case FormatCSV:
		return exportCSV(groups, outputPath)
	case FormatJSON:
		return exportJSON(groups, outputPath)
	default:
		return fmt.Errorf("unsupported export format: %s", format)
	}
}

func exportCSV(groups []duplicates.Group, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create csv file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"group_index", "hash", "size_bytes", "file_name", "file_path", "file_size_bytes"}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for i, group := range groups {
		for _, f := range group.Files {
			row := []string{
				fmt.Sprintf("%d", i+1),
				group.Hash,
				fmt.Sprintf("%d", group.Size),
				f.Name,
				f.Path,
				fmt.Sprintf("%d", f.Size),
			}
			if err := writer.Write(row); err != nil {
				return fmt.Errorf("write csv row: %w", err)
			}
		}
	}

	return nil
}

func exportJSON(groups []duplicates.Group, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create json file: %w", err)
	}
	defer file.Close()

	type groupEntry struct {
		GroupIndex int              `json:"groupIndex"`
		Hash       string           `json:"hash"`
		SizeBytes  int64            `json:"sizeBytes"`
		Files      []jsonFileRecord `json:"files"`
	}
	type payload struct {
		GeneratedAtUTC string       `json:"generatedAtUtc"`
		GroupCount     int          `json:"groupCount"`
		Groups         []groupEntry `json:"groups"`
	}

	out := payload{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		GroupCount:     len(groups),
		Groups:         make([]groupEntry, 0, len(groups)),
	}

	for i, group := range groups {
		entry := groupEntry{
			GroupIndex: i + 1,
			Hash:       group.Hash,
			SizeBytes:  group.Size,
			Files:      make([]jsonFileRecord, 0, len(group.Files)),
		}
		for _, f := range group.Files {
			entry.Files = append(entry.Files, jsonFileRecord{
				Name:      f.Name,
				Path:      f.Path,
				SizeBytes: f.Size,
			})
		}
		out.Groups = append(out.Groups, entry)
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		return fmt.Errorf("encode json report: %w", err)
	}
	return nil
}

type jsonFileRecord struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}
