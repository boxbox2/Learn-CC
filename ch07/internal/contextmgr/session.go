package contextmgr

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SessionState struct {
	ID      string
	RootDir string
	Ledger  *ReplacementLedger
	Files   *FileTracker
	Auto    *AutoTracker
	Usage   UsageAnchor

	mu sync.Mutex
}

func NewSessionState(workDir string) (*SessionState, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(workDir, ".mewcode", "sessions", id)
	state := &SessionState{
		ID:      id,
		RootDir: root,
		Ledger:  NewReplacementLedger(),
		Files:   NewFileTracker(),
		Auto:    NewAutoTracker(),
	}
	if err := os.MkdirAll(state.ToolResultsDir(), 0o755); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *SessionState) SessionDir() string {
	if s == nil {
		return ""
	}
	return s.RootDir
}

func (s *SessionState) ToolResultsDir() string {
	if s == nil {
		return ""
	}
	return filepath.Join(s.RootDir, "tool-results")
}

func (s *SessionState) Anchor() UsageAnchor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Usage
}

func (s *SessionState) SetAnchor(anchor UsageAnchor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Usage = anchor
}

func (s *SessionState) ClearAnchor() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Usage = UsageAnchor{}
}

func newSessionID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(b[:])), nil
}

type ReplacementLedger struct {
	seenIds      map[string]ReplacementDecision
	replacements map[string]string
	paths        map[string]string
	mu           sync.Mutex
}

func NewReplacementLedger() *ReplacementLedger {
	return &ReplacementLedger{
		seenIds:      map[string]ReplacementDecision{},
		replacements: map[string]string{},
		paths:        map[string]string{},
	}
}

func (l *ReplacementLedger) Decision(id string) (ReplacementDecision, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	decision, ok := l.seenIds[id]
	return decision, ok
}

func (l *ReplacementLedger) CommitKeep(id string, originalBytes int) ReplacementDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.seenIds[id]; ok {
		return existing
	}
	decision := ReplacementDecision{
		ToolUseID:     id,
		Action:        ReplacementActionKeep,
		OriginalBytes: originalBytes,
		DecidedAt:     time.Now(),
	}
	l.seenIds[id] = decision
	return decision
}

func (l *ReplacementLedger) CommitReplace(id string, originalBytes int, replacement, path string) ReplacementDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.seenIds[id]; ok {
		return existing
	}
	decision := ReplacementDecision{
		ToolUseID:     id,
		Action:        ReplacementActionReplace,
		OriginalBytes: originalBytes,
		Replacement:   replacement,
		Path:          path,
		DecidedAt:     time.Now(),
	}
	l.seenIds[id] = decision
	l.replacements[id] = replacement
	l.paths[id] = path
	return decision
}
