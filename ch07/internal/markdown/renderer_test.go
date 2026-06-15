package markdown

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	rendered, err := NewRenderer().Render("# Title\n\n- one\n- two", 40)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "Title") {
		t.Fatalf("rendered markdown missing title: %q", rendered)
	}
}
