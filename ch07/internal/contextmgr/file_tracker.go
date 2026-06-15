package contextmgr

import (
	"sort"
	"sync"
	"time"

	"mewcode/internal/tool"
)

type FileTracker struct {
	files map[string]FileSnapshot
	mu    sync.RWMutex
}

func NewFileTracker() *FileTracker {
	return &FileTracker{files: map[string]FileSnapshot{}}
}

func (f *FileTracker) Observe(results []tool.Result) {
	if f == nil {
		return
	}
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, result := range results {
		if result.Tool != "Read" || !result.OK {
			continue
		}
		path, _ := result.Data["path"].(string)
		content, _ := result.Data["content"].(string)
		if path == "" {
			continue
		}
		f.files[path] = FileSnapshot{Path: path, Content: content, ReadAt: now, Bytes: len(content)}
	}
}

func (f *FileTracker) Recent(n int) []FileSnapshot {
	if f == nil || n <= 0 {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	snapshots := make([]FileSnapshot, 0, len(f.files))
	for _, snapshot := range f.files {
		snapshots = append(snapshots, snapshot)
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].ReadAt.After(snapshots[j].ReadAt)
	})
	if len(snapshots) > n {
		snapshots = snapshots[:n]
	}
	return append([]FileSnapshot(nil), snapshots...)
}
