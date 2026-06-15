package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func WriteNote(domain *Domain, note Note, filename string) error {
	if domain == nil {
		return nil
	}
	if err := os.MkdirAll(domain.NotesDir, 0o755); err != nil {
		return err
	}
	if note.ID == "" {
		note.ID = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	if note.CreatedAt.IsZero() {
		note.CreatedAt = time.Now()
	}
	if note.UpdatedAt.IsZero() {
		note.UpdatedAt = note.CreatedAt
	}
	path := filepath.Join(domain.NotesDir, filename)
	body := fmt.Sprintf("---\nid: %q\ntype: %q\nscope: %q\ntitle: %q\ncreated_at: %q\nupdated_at: %q\nsource_session: %q\n---\n\n%s\n",
		note.ID,
		string(note.Type),
		string(note.Scope),
		note.Title,
		note.CreatedAt.Format(time.RFC3339),
		note.UpdatedAt.Format(time.RFC3339),
		note.SourceSession,
		strings.TrimSpace(note.Content),
	)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func DeleteNote(domain *Domain, filename string) error {
	if domain == nil {
		return nil
	}
	err := os.Remove(filepath.Join(domain.NotesDir, filename))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func ListNotes(domain *Domain) ([]Note, error) {
	if domain == nil {
		return nil, nil
	}
	entries, err := os.ReadDir(domain.NotesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var notes []Note
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(domain.NotesDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		notes = append(notes, parseNote(entry.Name(), string(data), domain.Scope))
	}
	return notes, nil
}

func parseNote(filename, data string, scope Scope) Note {
	note := Note{ID: strings.TrimSuffix(filename, filepath.Ext(filename)), Scope: scope}
	if parts := strings.SplitN(data, "---", 3); len(parts) == 3 {
		for _, line := range strings.Split(parts[1], "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			value = strings.Trim(strings.TrimSpace(value), `"`)
			switch strings.TrimSpace(key) {
			case "type":
				note.Type = NoteType(value)
			case "title":
				note.Title = value
			case "source_session":
				note.SourceSession = value
			}
		}
		note.Content = strings.TrimSpace(parts[2])
	}
	return note
}
