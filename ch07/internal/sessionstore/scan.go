package sessionstore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Summary struct {
	ID               string
	Title            string
	MessageCount     int
	UpdatedAt        time.Time
	CorruptLineCount int
}

type Diagnostic struct {
	Path    string
	Message string
}

func Scan(projectDir string) ([]Summary, error) {
	root := filepath.Join(projectDir, ".mewcode", "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var summaries []Summary
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		summary, err := scanFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func scanFile(path string) (Summary, error) {
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	summary := Summary{ID: id}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return summary, nil
		}
		return summary, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		var line Line
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil || line.Validate(id) != nil {
			summary.CorruptLineCount++
			continue
		}
		summary.MessageCount++
		if summary.Title == "" && line.Role == "user" && strings.TrimSpace(line.Content) != "" {
			summary.Title = firstLine(line.Content, 80)
		}
		summary.UpdatedAt = line.TS
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(strings.Split(s, "\n")[0])
	if len(s) > max {
		return s[:max]
	}
	return s
}
