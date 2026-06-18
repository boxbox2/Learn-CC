package sessionstore

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mewcode/internal/provider"
)

type Writer struct {
	id   string
	path string
	seq  int64
	mu   sync.Mutex
	now  func() time.Time
}

func Create(projectDir string) (*Writer, error) {
	id, err := NewID(time.Now(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return openWithID(projectDir, id, true)
}

func Open(projectDir, id string) (*Writer, error) {
	return openWithID(projectDir, id, false)
}

func openWithID(projectDir, id string, create bool) (*Writer, error) {
	root := filepath.Join(projectDir, ".mewcode", "sessions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(root, id+".jsonl")
	if create {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	w := &Writer{id: id, path: path, now: time.Now}
	summary, err := scanFile(path)
	if err != nil {
		return nil, err
	}
	w.seq = int64(summary.MessageCount)
	return w, nil
}

func (w *Writer) ID() string {
	if w == nil {
		return ""
	}
	return w.id
}

func (w *Writer) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

func (w *Writer) Append(msg provider.ChatMessage) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	line := LineFromMessage(w.id, w.seq, msg, w.now())
	data, err := json.Marshal(line)
	if err != nil {
		w.seq--
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		w.seq--
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		w.seq--
		return err
	}
	return nil
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return nil
}

func (w *Writer) Replace(messages []provider.ChatMessage) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var seq int64
	for _, msg := range messages {
		seq++
		line := LineFromMessage(w.id, seq, msg, w.now())
		data, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	w.seq = seq
	return nil
}
