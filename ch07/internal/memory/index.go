package memory

import (
	"os"
	"strings"
)

func ReadIndex(domain *Domain) (string, error) {
	if domain == nil {
		return "", nil
	}
	data, err := os.ReadFile(domain.Index)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func FormatIndexLine(note Note) string {
	desc := strings.TrimSpace(note.Content)
	desc = strings.ReplaceAll(desc, "\n", " ")
	if len(desc) > 120 {
		desc = desc[:120]
	}
	return "- [" + string(note.Type) + "] " + strings.TrimSpace(note.Title) + " - " + desc
}

func PromptIndex(user, project *Domain) string {
	var parts []string
	if text, err := ReadIndex(project); err == nil && strings.TrimSpace(text) != "" {
		parts = append(parts, "### Project Memory\n"+strings.TrimSpace(text))
	}
	if text, err := ReadIndex(user); err == nil && strings.TrimSpace(text) != "" {
		parts = append(parts, "### User Memory\n"+strings.TrimSpace(text))
	}
	return strings.Join(parts, "\n\n")
}

func IndexWithinLimit(content string) bool {
	if len([]byte(content)) > MaxIndexBytes {
		return false
	}
	if content == "" {
		return true
	}
	return len(strings.Split(strings.TrimRight(content, "\n"), "\n")) <= MaxIndexLines
}

func ClampIndex(content string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) > MaxIndexLines {
		lines = lines[:MaxIndexLines]
	}
	out := strings.Join(lines, "\n")
	for len([]byte(out)) > MaxIndexBytes && len(lines) > 0 {
		lines = lines[:len(lines)-1]
		out = strings.Join(lines, "\n")
	}
	if out != "" {
		out += "\n"
	}
	return out
}

func RewriteIndex(domain *Domain) error {
	notes, err := ListNotes(domain)
	if err != nil {
		return err
	}
	var lines []string
	for _, note := range notes {
		lines = append(lines, FormatIndexLine(note))
	}
	content := ClampIndex(strings.Join(lines, "\n"))
	if err := os.MkdirAll(domain.RootDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(domain.Index, []byte(content), 0o644)
}
