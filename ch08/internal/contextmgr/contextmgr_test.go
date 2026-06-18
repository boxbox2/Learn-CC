package contextmgr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

func TestOffloadSingleToolResult(t *testing.T) {
	store := NewToolResultStore(t.TempDir())
	ledger := NewReplacementLedger()
	content := strings.Repeat("x", SingleToolResultLimitBytes+1)
	messages := []provider.ChatMessage{{
		Role: provider.RoleUser,
		ToolResults: []provider.ToolResultMessage{{
			ID:      "call_1",
			Name:    "Read",
			Content: content,
		}},
	}}
	out, report, err := OffloadAndSnip(messages, ledger, store)
	if err != nil {
		t.Fatal(err)
	}
	if report.Replaced != 1 {
		t.Fatalf("replaced = %d, want 1", report.Replaced)
	}
	replacement := out[0].ToolResults[0].Content
	for _, want := range []string{"original_bytes:", "preview:", "path:", "read the file"} {
		if !strings.Contains(replacement, want) {
			t.Fatalf("replacement missing %q:\n%s", want, replacement)
		}
	}
	data, err := os.ReadFile(store.PathFor("call_1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatal("stored content differs from original")
	}
}

func TestOffloadAggregateLimitReplacesLargestMinimum(t *testing.T) {
	store := NewToolResultStore(t.TempDir())
	ledger := NewReplacementLedger()
	results := []provider.ToolResultMessage{
		{ID: "a", Name: "A", Content: strings.Repeat("a", 45000)},
		{ID: "b", Name: "B", Content: strings.Repeat("b", 45000)},
		{ID: "c", Name: "C", Content: strings.Repeat("c", 45000)},
		{ID: "d", Name: "D", Content: strings.Repeat("d", 45000)},
		{ID: "e", Name: "E", Content: strings.Repeat("e", 45000)},
	}
	out, report, err := OffloadAndSnip([]provider.ChatMessage{{Role: provider.RoleUser, ToolResults: results}}, ledger, store)
	if err != nil {
		t.Fatal(err)
	}
	if report.Replaced != 1 {
		t.Fatalf("replaced = %d, want 1", report.Replaced)
	}
	var replaced int
	for _, result := range out[0].ToolResults {
		if containsOffloadMarker(result.Content) {
			replaced++
		}
	}
	if replaced != 1 {
		t.Fatalf("offloaded results = %d, want 1", replaced)
	}
}

func TestOffloadDecisionFrozen(t *testing.T) {
	store := NewToolResultStore(t.TempDir())
	ledger := NewReplacementLedger()
	keep := []provider.ChatMessage{{Role: provider.RoleUser, ToolResults: []provider.ToolResultMessage{{ID: "same", Name: "Read", Content: "small"}}}}
	if _, _, err := OffloadAndSnip(keep, ledger, store); err != nil {
		t.Fatal(err)
	}
	large := []provider.ChatMessage{{Role: provider.RoleUser, ToolResults: []provider.ToolResultMessage{{ID: "same", Name: "Read", Content: strings.Repeat("x", SingleToolResultLimitBytes+1)}}}}
	out, _, err := OffloadAndSnip(large, ledger, store)
	if err != nil {
		t.Fatal(err)
	}
	if containsOffloadMarker(out[0].ToolResults[0].Content) {
		t.Fatal("kept decision flipped to replace")
	}
}

func TestPreviewLimits(t *testing.T) {
	content := strings.Repeat("一", 1000)
	preview := previewText(content)
	if len(preview) > PreviewMaxBytes {
		t.Fatalf("preview bytes = %d, want <= %d", len(preview), PreviewMaxBytes)
	}
	if !utf8.ValidString(preview) {
		t.Fatal("preview is not valid UTF-8")
	}
	lines := strings.Repeat("x\n", 30)
	preview = previewText(lines)
	if got := strings.Count(preview, "\n"); got > PreviewMaxLines {
		t.Fatalf("preview lines = %d, want <= %d", got, PreviewMaxLines)
	}
}

func TestEstimatorUsesAnchorAndIncrement(t *testing.T) {
	estimator := NewEstimator(100000)
	messages := []provider.ChatMessage{
		{Role: provider.RoleUser, Content: "old"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("x", 35)},
	}
	estimate := estimator.Estimate(messages, UsageAnchor{
		Usage:        provider.Usage{TotalTokens: 100},
		MessageCount: 1,
		Valid:        true,
	})
	if estimate.Source != "usage_anchor" || estimate.Tokens != 113 {
		t.Fatalf("estimate = %+v, want usage anchor 113", estimate)
	}
}

func TestSplitRecentKeepsToolPair(t *testing.T) {
	var messages []provider.ChatMessage
	for i := 0; i < 8; i++ {
		messages = append(messages, provider.ChatMessage{Role: provider.RoleUser, Content: strings.Repeat("x", 5000)})
	}
	messages = append(messages,
		provider.ChatMessage{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "Read"}}},
		provider.ChatMessage{Role: provider.RoleUser, ToolResults: []provider.ToolResultMessage{{ID: "call_1", Name: "Read", Content: "ok"}}},
	)
	_, recent := SplitRecent(messages, NewEstimator(100000))
	if len(recent) == 0 || len(recent[0].ToolResults) > 0 {
		t.Fatalf("recent starts with orphan tool result: %+v", recent)
	}
}

func TestRecoveryIncludesFilesToolsAndBoundary(t *testing.T) {
	recovery := BuildRecovery(RecoveryInputs{
		RecentFiles: []FileSnapshot{{Path: "a.go", Content: strings.Repeat("x", int(float64(RecentFileSnapshotTokens)*EstimateCharsPerToken)+10), Bytes: 10}},
		ToolDefinitions: []tool.Definition{{
			Name:        "Read",
			Description: "read files",
			Parameters:  tool.Schema{"type": "object"},
		}},
	})
	for _, want := range []string{"Recent Read File Snapshots", "Current Available Tools", "Boundary Prompt", "Read", "(content truncated)"} {
		if !strings.Contains(recovery, want) {
			t.Fatalf("recovery missing %q:\n%s", want, recovery)
		}
	}
}

func TestFileTrackerObservesReadOnly(t *testing.T) {
	tracker := NewFileTracker()
	tracker.Observe([]tool.Result{
		tool.Success("Read", "1", "ok", map[string]any{"path": "a.go", "content": "package a"}),
		tool.Success("Bash", "2", "ok", map[string]any{"path": "b.go", "content": "ignored"}),
		tool.Failure("Read", "3", "failed", "nope"),
	})
	recent := tracker.Recent(5)
	if len(recent) != 1 || recent[0].Path != "a.go" || recent[0].Content != "package a" {
		t.Fatalf("recent = %+v", recent)
	}
}

func TestAutoTrackerTripsAndResets(t *testing.T) {
	tracker := NewAutoTracker()
	for i := 0; i < AutoSummaryFailureLimit; i++ {
		tracker.RecordFailure(errors.New("boom"))
	}
	if !tracker.Tripped() {
		t.Fatal("tracker did not trip")
	}
	tracker.RecordSuccess()
	if tracker.Tripped() || tracker.Snapshot().Failures != 0 {
		t.Fatalf("tracker did not reset: %+v", tracker.Snapshot())
	}
}

func TestExtractSummaryDropsAnalysis(t *testing.T) {
	got, err := ExtractSummary("<analysis>draft</analysis><summary>final</summary>")
	if err != nil {
		t.Fatal(err)
	}
	if got != "final" {
		t.Fatalf("summary = %q, want final", got)
	}
}

func TestSummaryRequestHasNoTools(t *testing.T) {
	req := BuildSummaryChatRequest([]provider.ChatMessage{{Role: provider.RoleUser, Content: "hello"}}, config.ProviderConfig{Model: "fake"})
	if len(req.Tools) != 0 {
		t.Fatalf("summary tools = %+v, want none", req.Tools)
	}
	if !strings.Contains(req.SystemPrompt, "must not call any tools") {
		t.Fatalf("system prompt missing tool ban: %q", req.SystemPrompt)
	}
}

func TestManagerAutoCompacts(t *testing.T) {
	fp := &summaryProvider{}
	manager, err := NewManager(t.TempDir(), config.ProviderConfig{Protocol: config.ProtocolOpenAI, Model: "fake", ContextWindow: 40000}, fp)
	if err != nil {
		t.Fatal(err)
	}
	messages := []provider.ChatMessage{{Role: provider.RoleUser, Content: strings.Repeat("x", 30000)}}
	result, err := manager.ManageBeforeRequest(context.Background(), ManageRequest{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted {
		t.Fatal("expected auto compaction")
	}
	if len(fp.requests) != 1 || len(fp.requests[0].Tools) != 0 {
		t.Fatalf("summary requests = %+v", fp.requests)
	}
	if len(result.Messages) < 2 || !strings.Contains(result.Messages[0].Content, "Main request") {
		t.Fatalf("compacted messages = %+v", result.Messages)
	}
}

type summaryProvider struct {
	requests []provider.ChatRequest
}

func (s *summaryProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	s.requests = append(s.requests, req)
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: "<analysis>draft</analysis><summary>1. Main request and intent\nok\n2. Key technical concepts\n3. Files and code snippets\n4. Errors and fixes\n5. Problem-solving process\n6. User messages, preserving original wording when possible\n7. TODOs\n8. Current work\n9. Possible next steps</summary>"}
	ch <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	close(ch)
	return ch, nil
}
