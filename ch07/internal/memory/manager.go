package memory

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type Options struct {
	ProjectDir string
	HomeDir    string
	Provider   provider.Provider
	Config     config.ProviderConfig
}

type Manager struct {
	User      *Domain
	Project   *Domain
	Extractor Extractor
	LastError error
}

func NewManager(opts Options) (*Manager, error) {
	userRoot := filepath.Join(opts.HomeDir, ".mewcode", "memory")
	projectRoot := filepath.Join(opts.ProjectDir, ".mewcode", "memory")
	m := &Manager{
		User: &Domain{
			Scope:    ScopeUser,
			RootDir:  userRoot,
			NotesDir: filepath.Join(userRoot, "notes"),
			Index:    filepath.Join(userRoot, "index.md"),
		},
		Project: &Domain{
			Scope:    ScopeProject,
			RootDir:  projectRoot,
			NotesDir: filepath.Join(projectRoot, "notes"),
			Index:    filepath.Join(projectRoot, "index.md"),
		},
		Extractor: Extractor{Provider: opts.Provider, Config: opts.Config},
	}
	for _, domain := range []*Domain{m.User, m.Project} {
		if err := os.MkdirAll(domain.NotesDir, 0o755); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) PromptIndex(ctx context.Context) string {
	if m == nil {
		return ""
	}
	return PromptIndex(m.User, m.Project)
}

func (m *Manager) UpdateAsync(snapshot Snapshot) {
	if m == nil {
		return
	}
	snapshot.Messages = cloneMessages(snapshot.Messages)
	go func() {
		if err := m.Update(context.Background(), snapshot); err != nil {
			m.LastError = err
			log.Printf("memory update failed: %v", err)
		}
	}()
}

func (m *Manager) Update(ctx context.Context, snapshot Snapshot) error {
	index := m.PromptIndex(ctx)
	changes, err := m.Extractor.Extract(ctx, snapshot, index)
	if err != nil {
		return err
	}
	return m.Apply(ctx, changes, snapshot)
}

func (m *Manager) Apply(ctx context.Context, changes ChangeSet, snapshot Snapshot) error {
	if m == nil {
		return nil
	}
	for _, change := range changes.Changes {
		if err := ValidateChange(change); err != nil {
			return err
		}
		if change.Action == ActionNoop {
			continue
		}
		domain := m.Project
		if change.Scope == ScopeUser {
			domain = m.User
		}
		domain.mu.Lock()
		err := m.applyOne(domain, change, snapshot)
		domain.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) applyOne(domain *Domain, change Change, snapshot Snapshot) error {
	switch change.Action {
	case ActionDelete:
		if err := DeleteNote(domain, change.Filename); err != nil {
			return err
		}
	default:
		now := time.Now()
		note := Note{
			ID:            change.Filename[:len(change.Filename)-len(filepath.Ext(change.Filename))],
			Type:          change.Type,
			Scope:         change.Scope,
			Title:         change.Title,
			Content:       change.Content,
			CreatedAt:     now,
			UpdatedAt:     now,
			SourceSession: snapshot.SessionID,
		}
		if err := WriteNote(domain, note, change.Filename); err != nil {
			return err
		}
	}
	if err := RewriteIndex(domain); err != nil {
		return err
	}
	text, err := ReadIndex(domain)
	if err != nil {
		return err
	}
	if !IndexWithinLimit(text) {
		return os.WriteFile(domain.Index, []byte(ClampIndex(text)), 0o644)
	}
	return nil
}
