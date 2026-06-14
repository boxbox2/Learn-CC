package permission

import (
	"fmt"
	"regexp"
)

var dangerousCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|[;&|]\s*)rm\s+(-[a-z]*r[a-z]*f|-rf|-fr)\s*$`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)rm\s+(-[a-z]*r[a-z]*f|-rf|-fr)\s+(/|\*|\.{2}(/|\\|\*)?)`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)del\s+.*(/s|/q).*(\\|/|\*|\.\.)`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)rmdir\s+.*(/s).*(\\|/|\*|\.\.)`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)format(\.com)?\b`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)mkfs(\.[a-z0-9]+)?\b`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)dd\s+.*\bof=(/dev/|\\\\\.\\|[a-z]:\\)`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)chmod\s+-R\s+777\s+(/|\\|\*)`),
	regexp.MustCompile(`(?i)(^|[;&|]\s*)chown\s+-R\s+\S+\s+(/|\\|\*)`),
	regexp.MustCompile(`(?i)Remove-Item\b.*\b-Recurse\b.*\b-Force\b.*(\s/|\s\\|\s\*|\.\.)`),
	regexp.MustCompile(`(?i)Remove-Item\b.*\b-Force\b.*\b-Recurse\b.*(\s/|\s\\|\s\*|\.\.)`),
}

func CheckHardBlock(req Request) Decision {
	if req.Tool != "Bash" {
		return Decision{Status: DecisionAsk, Reason: "no hard block applies"}
	}
	command := req.Fields["command"]
	for _, pattern := range dangerousCommandPatterns {
		if pattern.MatchString(command) {
			return Decision{
				Status: DecisionDeny,
				Reason: fmt.Sprintf("command %q matched a non-overridable dangerous command pattern", command),
				Source: SourceHardBlock,
			}
		}
	}
	return Decision{Status: DecisionAsk, Reason: "no hard block applies"}
}
