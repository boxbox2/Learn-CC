package contextmgr

import (
	"encoding/json"
	"fmt"
	"strings"

	"mewcode/internal/tool"
)

const BoundaryPrompt = "Need exact file contents, exact errors, user wording, or full tool output? Re-read the referenced file or offloaded tool-result path with the file reading tool. Do not infer code details from the summary alone."

type RecoveryInputs struct {
	RecentFiles     []FileSnapshot
	ToolDefinitions []tool.Definition
	BoundaryText    string
}

func BuildRecovery(inputs RecoveryInputs) string {
	var b strings.Builder
	b.WriteString("## Recent Read File Snapshots\n")
	if len(inputs.RecentFiles) == 0 {
		b.WriteString("No recent file snapshots recorded.\n")
	} else {
		files := inputs.RecentFiles
		if len(files) > MaxRecentFiles {
			files = files[:MaxRecentFiles]
		}
		for _, file := range files {
			content := truncateSnapshot(file.Content)
			b.WriteString(fmt.Sprintf("\n### %s\nread_at: %s\nbytes: %d\n```text\n%s\n```\n", file.Path, file.ReadAt.Format("2006-01-02T15:04:05Z07:00"), file.Bytes, content))
		}
	}
	b.WriteString("\n## Current Available Tools\n")
	if len(inputs.ToolDefinitions) == 0 {
		b.WriteString("No tools are currently available.\n")
	} else {
		for _, def := range inputs.ToolDefinitions {
			data, _ := json.Marshal(def.Parameters)
			b.WriteString(fmt.Sprintf("- %s: %s\n  schema: %s\n", def.Name, def.Description, string(data)))
		}
	}
	b.WriteString("\n## Boundary Prompt\n")
	text := inputs.BoundaryText
	if strings.TrimSpace(text) == "" {
		text = BoundaryPrompt
	}
	b.WriteString(text)
	b.WriteString("\n")
	return b.String()
}

func truncateSnapshot(content string) string {
	limit := int(float64(RecentFileSnapshotTokens) * EstimateCharsPerToken)
	if len(content) <= limit {
		return content
	}
	return truncateUTF8Bytes(content, limit) + "\n(content truncated)"
}
