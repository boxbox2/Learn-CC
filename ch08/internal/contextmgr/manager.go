package contextmgr

import (
	"context"
	"fmt"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type Manager struct {
	Provider       provider.Provider
	ProviderConfig config.ProviderConfig
	WorkDir        string
	Session        *SessionState
	Store          *ToolResultStore
	Tracker        *FileTracker
	Estimator      *Estimator
	Summarizer     *Summarizer
	Auto           *AutoTracker
}

type ManageRequest struct {
	Messages     []provider.ChatMessage
	AllowedTools []tool.Definition
	Mode         ManageMode
}

type ManageResult struct {
	Messages      []provider.ChatMessage
	OffloadReport OffloadReport
	Before        Estimate
	After         Estimate
	Compacted     bool
	Mode          ManageMode
}

func NewManager(workDir string, cfg config.ProviderConfig, p provider.Provider) (*Manager, error) {
	session, err := NewSessionState(workDir)
	if err != nil {
		return nil, err
	}
	contextWindow := config.ContextWindowFor(cfg)
	manager := &Manager{
		Provider:       p,
		ProviderConfig: cfg,
		WorkDir:        workDir,
		Session:        session,
		Store:          NewToolResultStore(session.ToolResultsDir()),
		Tracker:        session.Files,
		Estimator:      NewEstimator(contextWindow),
		Summarizer:     NewSummarizer(p, cfg),
		Auto:           session.Auto,
	}
	return manager, nil
}

func (m *Manager) ManageBeforeRequest(ctx context.Context, req ManageRequest) (ManageResult, error) {
	if m == nil {
		return ManageResult{Messages: cloneMessages(req.Messages)}, nil
	}
	messages, report, err := OffloadAndSnip(req.Messages, m.Session.Ledger, m.Store)
	if err != nil {
		return ManageResult{}, err
	}
	before := m.Estimator.Estimate(messages, m.Session.Anchor())
	result := ManageResult{Messages: messages, OffloadReport: report, Before: before, After: before, Mode: ManageModeAuto}
	if before.Tokens < m.Estimator.ContextWindow-SummaryOutputReserveTokens-AutoSafetyMarginTokens {
		return result, nil
	}
	if m.Auto.Tripped() {
		return result, nil
	}
	compacted, summary, err := m.summarizeAndRestore(ctx, messages, req.AllowedTools, SummaryModeAuto)
	if err != nil {
		m.Auto.RecordFailure(err)
		return result, nil
	}
	m.Auto.RecordSuccess()
	after := m.Estimator.Estimate(compacted, UsageAnchor{})
	summary.EstimateBefore = before
	summary.EstimateAfter = after
	return ManageResult{Messages: compacted, OffloadReport: report, Before: before, After: after, Compacted: true, Mode: ManageModeAuto}, nil
}

func (m *Manager) ForceCompact(ctx context.Context, messages []provider.ChatMessage, allowedTools []tool.Definition) (ManageResult, error) {
	before := m.Estimator.Estimate(messages, m.Session.Anchor())
	compacted, _, err := m.summarizeAndRestore(ctx, messages, allowedTools, SummaryModeManual)
	if err != nil {
		return ManageResult{Messages: cloneMessages(messages), Before: before, After: before, Mode: ManageModeManual}, err
	}
	after := m.Estimator.Estimate(compacted, UsageAnchor{})
	return ManageResult{Messages: compacted, Before: before, After: after, Compacted: true, Mode: ManageModeManual}, nil
}

func (m *Manager) EmergencyCompact(ctx context.Context, messages []provider.ChatMessage, allowedTools []tool.Definition) (ManageResult, error) {
	offloaded, report, err := OffloadAndSnip(messages, m.Session.Ledger, m.Store)
	if err != nil {
		return ManageResult{}, err
	}
	before := m.Estimator.Estimate(offloaded, m.Session.Anchor())
	compacted, _, err := m.summarizeAndRestore(ctx, offloaded, allowedTools, SummaryModeEmergency)
	if err != nil {
		return ManageResult{Messages: offloaded, OffloadReport: report, Before: before, After: before, Mode: ManageModeEmergency}, err
	}
	m.Session.ClearAnchor()
	after := m.Estimator.Estimate(compacted, UsageAnchor{})
	if after.Tokens >= m.Estimator.ContextWindow-ManualSafetyMarginTokens {
		return ManageResult{Messages: compacted, OffloadReport: report, Before: before, After: after, Compacted: true, Mode: ManageModeEmergency}, fmt.Errorf("context remains too large after emergency compaction: %d tokens", after.Tokens)
	}
	return ManageResult{Messages: compacted, OffloadReport: report, Before: before, After: after, Compacted: true, Mode: ManageModeEmergency}, nil
}

func (m *Manager) RecordUsage(usage provider.Usage, messageCount int) {
	if m == nil || usage.IsZero() {
		return
	}
	m.Session.SetAnchor(UsageAnchor{Usage: usage, MessageCount: messageCount, Valid: true})
}

func (m *Manager) ObserveToolResults(results []tool.Result) {
	if m == nil || m.Tracker == nil {
		return
	}
	m.Tracker.Observe(results)
}

func (m *Manager) summarizeAndRestore(ctx context.Context, messages []provider.ChatMessage, allowedTools []tool.Definition, mode SummaryMode) ([]provider.ChatMessage, SummaryResult, error) {
	older, recent := SplitRecent(messages, m.Estimator)
	if len(older) == 0 {
		older = cloneMessages(messages)
		recent = nil
	}
	result, err := m.Summarizer.Summarize(ctx, SummaryRequest{
		Messages:        older,
		Tools:           nil,
		Mode:            mode,
		ContextWindow:   m.Estimator.ContextWindow,
		SafetyMargin:    safetyMarginForSummary(mode),
		ToolDefinitions: allowedTools,
		RecentFiles:     m.Tracker.Recent(MaxRecentFiles),
	})
	if err != nil {
		return nil, SummaryResult{}, err
	}
	recovery := BuildRecovery(RecoveryInputs{
		RecentFiles:     m.Tracker.Recent(MaxRecentFiles),
		ToolDefinitions: allowedTools,
		BoundaryText:    BoundaryPrompt,
	})
	compacted := []provider.ChatMessage{
		{Role: provider.RoleSystem, Content: result.Summary},
		{Role: provider.RoleSystem, Content: recovery},
	}
	compacted = append(compacted, recent...)
	result.Messages = cloneMessages(compacted)
	result.Recovery = recovery
	result.Recent = cloneMessages(recent)
	return compacted, result, nil
}

func safetyMarginForSummary(mode SummaryMode) int {
	switch mode {
	case SummaryModeManual:
		return ManualSafetyMarginTokens
	default:
		return 0
	}
}
