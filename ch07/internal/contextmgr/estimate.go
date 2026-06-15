package contextmgr

import (
	"encoding/json"
	"math"

	"mewcode/internal/provider"
)

type Estimator struct {
	ContextWindow int
}

func NewEstimator(contextWindow int) *Estimator {
	return &Estimator{ContextWindow: contextWindow}
}

func (e *Estimator) Estimate(messages []provider.ChatMessage, anchor UsageAnchor) Estimate {
	messageCount := len(messages)
	if anchor.Valid && messageCount >= anchor.MessageCount {
		incrementBytes := MessagesBytes(messages[anchor.MessageCount:])
		increment := EstimateBytes(incrementBytes)
		anchorTokens := usageTokens(anchor.Usage)
		return Estimate{
			Tokens:          anchorTokens + increment,
			Source:          "usage_anchor",
			AnchorTokens:    anchorTokens,
			IncrementTokens: increment,
			MessageCount:    messageCount,
			ContextWindow:   e.ContextWindow,
		}
	}
	tokens := EstimateBytes(MessagesBytes(messages))
	return Estimate{Tokens: tokens, Source: "full_scan", MessageCount: messageCount, ContextWindow: e.ContextWindow}
}

func EstimateBytes(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return int(math.Ceil(float64(bytes) / EstimateCharsPerToken))
}

func MessagesBytes(messages []provider.ChatMessage) int {
	total := 0
	for _, message := range messages {
		total += MessageBytes(message)
	}
	return total
}

func MessageBytes(message provider.ChatMessage) int {
	total := len(message.Content) + len(message.Role)
	for _, call := range message.ToolCalls {
		total += len(call.ID) + len(call.Name) + len(call.Arguments)
	}
	for _, result := range message.ToolResults {
		total += len(result.ID) + len(result.Name) + len(result.Content)
	}
	return total
}

func usageTokens(usage provider.Usage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.PromptTokens + usage.CachedTokens + usage.CompletionTokens
}

func ToolDefinitionBytes(def any) int {
	data, err := json.Marshal(def)
	if err != nil {
		return 0
	}
	return len(data)
}
