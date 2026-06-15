package contextmgr

import "mewcode/internal/provider"

func SplitRecent(messages []provider.ChatMessage, estimator *Estimator) (older, recent []provider.ChatMessage) {
	if len(messages) <= RecentMinMessages {
		return nil, cloneMessages(messages)
	}
	if estimator == nil {
		estimator = NewEstimator(0)
	}
	start := len(messages)
	tokens := 0
	count := 0
	for start > 0 {
		start--
		count++
		tokens += EstimateBytes(MessageBytes(messages[start]))
		if tokens >= RecentMinTokens && count >= RecentMinMessages {
			break
		}
	}
	start = expandToolBoundary(messages, start)
	return cloneMessages(messages[:start]), cloneMessages(messages[start:])
}

func expandToolBoundary(messages []provider.ChatMessage, start int) int {
	for start > 0 && len(messages[start].ToolResults) > 0 {
		start--
		if len(messages[start].ToolCalls) > 0 {
			break
		}
	}
	return start
}
