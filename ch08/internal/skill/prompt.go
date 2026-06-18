package skill

import (
	"fmt"
	"strings"
)

func (c *Catalog) PromptCatalog() string {
	summaries := c.Summaries()
	if len(summaries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Available Skills\n\n")
	for _, summary := range summaries {
		fmt.Fprintf(&b, "- %s: %s\n", summary.Name, summary.Description)
	}
	b.WriteString("\nCall the LoadSkill tool with {\"name\": \"<skill_name>\"} to activate a skill's full SOP and specialized tools before executing it.")
	return strings.TrimSpace(b.String())
}
