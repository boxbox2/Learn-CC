package provider

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type ToolResultMessage struct {
	ID      string
	Name    string
	Content string
}
