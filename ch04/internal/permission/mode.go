package permission

import (
	"fmt"

	"mewcode/internal/tool"
)

func EvaluateMode(mode Mode, req Request) Decision {
	if mode == "" {
		mode = ModeDefault
	}
	switch mode {
	case ModeDefault, ModePlan:
		if req.Safety == tool.SafetyReadOnly {
			return Decision{Status: DecisionAllow, Reason: fmt.Sprintf("%s mode allows read-only tool", mode), Source: SourceMode}
		}
		return Decision{Status: DecisionAsk, Reason: fmt.Sprintf("%s mode requires confirmation", mode), Source: SourceMode}
	case ModeAcceptEdits:
		if req.Tool == "Bash" {
			return Decision{Status: DecisionAsk, Reason: "acceptEdits mode requires Bash confirmation", Source: SourceMode}
		}
		return Decision{Status: DecisionAllow, Reason: "acceptEdits mode allows file and read tools", Source: SourceMode}
	case ModeBypassPermissions:
		return Decision{Status: DecisionAllow, Reason: "bypassPermissions mode allows unmatched tool calls", Source: SourceMode}
	default:
		if req.Safety == tool.SafetyReadOnly {
			return Decision{Status: DecisionAllow, Reason: "default mode allows read-only tool", Source: SourceMode}
		}
		return Decision{Status: DecisionAsk, Reason: "default mode requires confirmation", Source: SourceMode}
	}
}
