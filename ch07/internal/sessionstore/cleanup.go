package sessionstore

import (
	"os"
	"path/filepath"
	"time"
)

type CleanupReport struct {
	Deleted []string
	Failed  []Diagnostic
}

func Cleanup(projectDir string, olderThan time.Duration, now time.Time) (CleanupReport, error) {
	var report CleanupReport
	root := filepath.Join(projectDir, ".mewcode", "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		summary, err := scanFile(path)
		if err != nil {
			report.Failed = append(report.Failed, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if summary.UpdatedAt.IsZero() || now.Sub(summary.UpdatedAt) <= olderThan {
			continue
		}
		if err := os.Remove(path); err != nil {
			report.Failed = append(report.Failed, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		report.Deleted = append(report.Deleted, path)
	}
	return report, nil
}
