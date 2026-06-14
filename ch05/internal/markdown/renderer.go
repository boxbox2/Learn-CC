package markdown

import (
	"fmt"

	"github.com/charmbracelet/glamour"
)

type Renderer struct{}

func NewRenderer() Renderer {
	return Renderer{}
}

func (Renderer) Render(markdown string, width int) (string, error) {
	if width < 20 {
		width = 20
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return markdown, fmt.Errorf("create markdown renderer: %w", err)
	}
	out, err := renderer.Render(markdown)
	if err != nil {
		return markdown, err
	}
	return out, nil
}
