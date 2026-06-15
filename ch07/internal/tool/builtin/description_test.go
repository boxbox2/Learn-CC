package builtin

import (
	"strings"
	"testing"
)

func TestToolDescriptionsReinforceRules(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{"Read", ReadTool{}.Definition().Description, "before explaining"},
		{"Glob", GlobTool{}.Definition().Description, "locating files"},
		{"Grep", GrepTool{}.Definition().Description, "call sites"},
		{"Write", WriteTool{}.Definition().Description, "path and intent"},
		{"Edit", EditTool{}.Definition().Description, "Read the file first"},
		{"Bash", BashTool{}.Definition().Description, "verification commands"},
	}
	for _, tt := range tests {
		if !strings.Contains(tt.description, tt.want) {
			t.Fatalf("%s description %q missing %q", tt.name, tt.description, tt.want)
		}
	}
}
