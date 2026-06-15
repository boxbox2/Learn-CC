package sessionstore

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mewcode/internal/provider"
)

func TestNewID(t *testing.T) {
	id, err := NewID(time.Date(2026, 6, 15, 16, 0, 1, 0, time.UTC), strings.NewReader("\x01\x02"))
	if err != nil {
		t.Fatal(err)
	}
	if id != "20260615-160001-0102" || !ValidID(id) {
		t.Fatalf("id = %q", id)
	}
}

func TestWriterAppendAndScan(t *testing.T) {
	w, err := Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Append(provider.ChatMessage{Role: provider.RoleUser, Content: "hello"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Fatalf("lines = %d", len(lines))
	}
	seen := map[int64]bool{}
	for _, raw := range lines {
		var line Line
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatal(err)
		}
		if seen[line.Seq] {
			t.Fatalf("duplicate seq %d", line.Seq)
		}
		seen[line.Seq] = true
	}
}

func TestWriterClose(t *testing.T) {
	var nilWriter *Writer
	if err := nilWriter.Close(); err != nil {
		t.Fatal(err)
	}
	w, err := Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSkipsBadLineAndTruncatesTools(t *testing.T) {
	dir := t.TempDir()
	id, _ := NewID(time.Now(), rand.Reader)
	path := filepath.Join(dir, ".mewcode", "sessions", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		mustJSON(t, LineFromMessage(id, 1, provider.ChatMessage{Role: provider.RoleUser, Content: "start"}, time.Now())),
		"{bad",
		mustJSON(t, LineFromMessage(id, 2, provider.ChatMessage{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "Read"}}}, time.Now())),
		mustJSON(t, LineFromMessage(id, 3, provider.ChatMessage{Role: provider.RoleAssistant, Content: "lost"}, time.Now())),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Restore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Messages) != 1 || result.Summary.CorruptLineCount != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCleanup(t *testing.T) {
	project := t.TempDir()
	old, _ := OpenWithFixedIDForTest(project, "20260501-120000-0001")
	old.now = func() time.Time { return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) }
	if err := old.Append(provider.ChatMessage{Role: provider.RoleUser, Content: "old"}); err != nil {
		t.Fatal(err)
	}
	newer, _ := OpenWithFixedIDForTest(project, "20260610-120000-0001")
	newer.now = func() time.Time { return time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC) }
	if err := newer.Append(provider.ChatMessage{Role: provider.RoleUser, Content: "new"}); err != nil {
		t.Fatal(err)
	}
	report, err := Cleanup(project, 30*24*time.Hour, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Deleted) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(newer.Path()); err != nil {
		t.Fatal(err)
	}
}

func OpenWithFixedIDForTest(project, id string) (*Writer, error) {
	return openWithID(project, id, true)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
