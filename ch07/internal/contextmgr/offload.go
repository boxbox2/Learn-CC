package contextmgr

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"mewcode/internal/provider"
)

func OffloadAndSnip(messages []provider.ChatMessage, ledger *ReplacementLedger, store *ToolResultStore) ([]provider.ChatMessage, OffloadReport, error) {
	out := cloneMessages(messages)
	var report OffloadReport
	for mi := range out {
		if len(out[mi].ToolResults) == 0 {
			continue
		}
		candidates := make([]toolResultCandidate, 0, len(out[mi].ToolResults))
		for ri := range out[mi].ToolResults {
			result := out[mi].ToolResults[ri]
			size := len(result.Content)
			report.Examined++
			report.BytesBefore += size
			candidates = append(candidates, toolResultCandidate{index: ri, id: result.ID, bytes: size})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].bytes == candidates[j].bytes {
				return candidates[i].index < candidates[j].index
			}
			return candidates[i].bytes > candidates[j].bytes
		})

		replaced := map[int]bool{}
		decidedKeep := map[int]bool{}
		for _, candidate := range candidates {
			decision, ok := ledger.Decision(candidate.id)
			if !ok {
				continue
			}
			report.SkippedKnown++
			switch decision.Action {
			case ReplacementActionReplace:
				out[mi].ToolResults[candidate.index].Content = decision.Replacement
				replaced[candidate.index] = true
				report.Replaced++
			case ReplacementActionKeep:
				decidedKeep[candidate.index] = true
				report.Kept++
			}
		}

		for _, candidate := range candidates {
			if replaced[candidate.index] || decidedKeep[candidate.index] {
				continue
			}
			if candidate.bytes > SingleToolResultLimitBytes {
				if replaceCandidate(&out[mi].ToolResults[candidate.index], candidate.bytes, ledger, store, &report) {
					replaced[candidate.index] = true
				}
			}
		}

		aggregate := 0
		for _, candidate := range candidates {
			if replaced[candidate.index] || decidedKeep[candidate.index] {
				continue
			}
			aggregate += candidate.bytes
		}
		for _, candidate := range candidates {
			if aggregate <= MessageAggregateLimitBytes {
				break
			}
			if replaced[candidate.index] || decidedKeep[candidate.index] {
				continue
			}
			if replaceCandidate(&out[mi].ToolResults[candidate.index], candidate.bytes, ledger, store, &report) {
				replaced[candidate.index] = true
				aggregate -= candidate.bytes
			}
		}

		for _, candidate := range candidates {
			if replaced[candidate.index] || decidedKeep[candidate.index] {
				continue
			}
			ledger.CommitKeep(candidate.id, candidate.bytes)
			report.Kept++
		}
		for ri := range out[mi].ToolResults {
			report.BytesAfter += len(out[mi].ToolResults[ri].Content)
		}
	}
	return out, report, nil
}

type toolResultCandidate struct {
	index int
	id    string
	bytes int
}

func replaceCandidate(result *provider.ToolResultMessage, originalBytes int, ledger *ReplacementLedger, store *ToolResultStore, report *OffloadReport) bool {
	if decision, ok := ledger.Decision(result.ID); ok {
		if decision.Action == ReplacementActionReplace {
			result.Content = decision.Replacement
			report.Replaced++
			return true
		}
		report.Kept++
		return false
	}
	path, _, err := store.Write(result.ID, result.Content)
	if err != nil {
		report.WriteFailures = append(report.WriteFailures, result.ID)
		return false
	}
	replacement := BuildPreviewReplacement(result.ID, result.Name, result.Content, path)
	decision := ledger.CommitReplace(result.ID, originalBytes, replacement, path)
	if decision.Action != ReplacementActionReplace {
		if decision.Action == ReplacementActionKeep {
			report.Kept++
			return false
		}
	}
	result.Content = decision.Replacement
	report.Replaced++
	return true
}

func BuildPreviewReplacement(toolUseID, toolName, content, path string) string {
	return fmt.Sprintf(
		"[tool result offloaded]\n"+
			"tool_use_id: %s\n"+
			"tool_name: %s\n"+
			"original_bytes: %d\n"+
			"path: %s\n"+
			"preview:\n%s\n"+
			"[end preview]\n"+
			"Full content is stored on disk. If exact details are needed, read the file at the path above.",
		toolUseID,
		toolName,
		len(content),
		path,
		previewText(content),
	)
}

func previewText(content string) string {
	lineLimited := firstNLines(content, PreviewMaxLines)
	return truncateUTF8Bytes(lineLimited, PreviewMaxBytes)
}

func firstNLines(s string, maxLines int) string {
	if maxLines <= 0 || s == "" {
		return ""
	}
	lines := 0
	for i, r := range s {
		if r == '\n' {
			lines++
			if lines >= maxLines {
				return s[:i+1]
			}
		}
	}
	return s
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func cloneMessages(messages []provider.ChatMessage) []provider.ChatMessage {
	out := append([]provider.ChatMessage(nil), messages...)
	for i := range out {
		out[i].ToolCalls = append([]provider.ToolCall(nil), out[i].ToolCalls...)
		out[i].ToolResults = append([]provider.ToolResultMessage(nil), out[i].ToolResults...)
	}
	return out
}

func containsOffloadMarker(content string) bool {
	return strings.Contains(content, "[tool result offloaded]")
}
