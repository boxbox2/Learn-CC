package contextmgr

import (
	"time"

	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type ManageMode string

const (
	ManageModeAuto      ManageMode = "auto"
	ManageModeManual    ManageMode = "manual"
	ManageModeEmergency ManageMode = "emergency"
)

type ReplacementAction string

const (
	ReplacementActionKeep    ReplacementAction = "keep"
	ReplacementActionReplace ReplacementAction = "replace"
)

type ReplacementDecision struct {
	ToolUseID     string
	Action        ReplacementAction
	OriginalBytes int
	Replacement   string
	Path          string
	DecidedAt     time.Time
}

type OffloadReport struct {
	Examined      int
	Replaced      int
	Kept          int
	SkippedKnown  int
	WriteFailures []string
	BytesBefore   int
	BytesAfter    int
}

type UsageAnchor struct {
	Usage        provider.Usage
	MessageCount int
	Valid        bool
}

type Estimate struct {
	Tokens          int
	Source          string
	AnchorTokens    int
	IncrementTokens int
	MessageCount    int
	ContextWindow   int
}

type FileSnapshot struct {
	Path    string
	Content string
	ReadAt  time.Time
	Bytes   int
}

type ToolSnapshot struct {
	Definitions []tool.Definition
}

type SummaryMode string

const (
	SummaryModeAuto      SummaryMode = "auto"
	SummaryModeManual    SummaryMode = "manual"
	SummaryModeEmergency SummaryMode = "emergency"
)

type SummaryRequest struct {
	Messages        []provider.ChatMessage
	Tools           []tool.Definition
	Mode            SummaryMode
	ContextWindow   int
	SafetyMargin    int
	RecentFiles     []FileSnapshot
	ToolDefinitions []tool.Definition
}

type SummaryResult struct {
	Messages       []provider.ChatMessage
	Summary        string
	Recovery       string
	Recent         []provider.ChatMessage
	DroppedGroups  int
	EstimateBefore Estimate
	EstimateAfter  Estimate
}

type AutoStatus struct {
	Failures  int
	Tripped   bool
	LastError string
}
