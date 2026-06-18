package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mewcode/internal/chat"
	"mewcode/internal/command"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/sessionstore"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.textarea.SetWidth(msg.Width)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case streamMsg:
		return m.handleStream(provider.StreamEvent(msg))
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		m.denyAllPermissions()
		if m.Status == StatusStreaming && m.StreamCancel != nil {
			m.StreamCancel()
			m.Status = StatusIdle
			m.StreamCancel = nil
			return m, tea.Println(m.Current.Content)
		}
		return m, tea.Quit
	case keyThinking:
		m.ShowThinking = !m.ShowThinking
		return m, nil
	case keyRetry:
		if m.Status == StatusStreaming {
			return m, nil
		}
		return m.startRetry()
	case keyEnter:
		if m.ActivePermission != nil && !strings.EqualFold(strings.TrimSpace(m.textarea.Value()), "/exit") {
			return m, nil
		}
		if m.Completion.Active {
			return m.acceptCompletion()
		}
		return m.startSubmit()
	}
	if m.ActivePermission != nil {
		return m.handlePermissionKey(msg)
	}
	if m.Completion.Active {
		if handled, next, cmd := m.handleCompletionKey(msg); handled {
			return next, cmd
		}
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refreshCompletion()
	return m, cmd
}

func (m Model) startSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	parsed := m.Commands.Parse(input)
	if parsed.Empty {
		return m, nil
	}
	if parsed.Chat {
		return m.startUserMessage(parsed.Input)
	}
	if parsed.Unknown != "" {
		m.showLocalError(fmt.Sprintf("Unknown command %q. Use /help to see available commands.", parsed.Unknown))
		m.textarea.Reset()
		m.refreshCompletion()
		return m, nil
	}
	if parsed.Command != nil {
		return m.dispatchCommand(*parsed.Command)
	}
	return m, nil
}

func (m Model) startUserMessage(input string) (Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	events, err := m.Runner.Submit(ctx, input, submitMode(m.ChatMode))
	if err != nil {
		cancel()
		m.Status = StatusError
		m.LastError = err.Error()
		m.Current.ErrorText = err.Error()
		return m, nil
	}
	m.StreamCancel = cancel
	m.events = events
	m.Current = UIMessage{Role: provider.RoleAssistant, Status: MessageStatusStreaming}
	m.Status = StatusStreaming
	m.textarea.Reset()
	m.refreshCompletion()
	m.Output = append(m.Output, userBlock(input))
	return m, waitForEvent(events)
}

func (m Model) dispatchCommand(inv command.Invocation) (tea.Model, tea.Cmd) {
	if !command.CanExecute(inv.Definition.Kind, m.agentState()) {
		m.showLocalError("请等待当前任务完成")
		m.textarea.Reset()
		m.refreshCompletion()
		return m, nil
	}
	ctx := context.Background()
	ctrl := &commandController{model: &m}
	result, err := inv.Definition.Handler(ctx, inv, ctrl)
	if err != nil {
		m.Status = StatusError
		m.LastError = err.Error()
		m.Current.ErrorText = err.Error()
		m.showLocalError(err.Error())
		m.textarea.Reset()
		m.refreshCompletion()
		return m, nil
	}
	_ = result
	m.textarea.Reset()
	m.refreshCompletion()
	return m, ctrl.cmd
}

func submitMode(mode command.ChatMode) chat.SubmitMode {
	if mode == command.ChatModePlan {
		return chat.SubmitModePlan
	}
	return chat.SubmitModeDefault
}

func (m Model) agentState() command.AgentState {
	if m.Status == StatusStreaming {
		return command.AgentStateRunning
	}
	return command.AgentStateIdle
}

func (m *Model) showLocalMessage(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	m.Output = append(m.Output, message)
}

func (m *Model) showLocalError(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	m.Output = append(m.Output, errorBlock(message))
}

type commandController struct {
	model *Model
	cmd   tea.Cmd
}

func (c *commandController) AgentState() command.AgentState {
	return c.model.agentState()
}

func (c *commandController) Mode() command.ChatMode {
	return c.model.ChatMode
}

func (c *commandController) SetMode(mode command.ChatMode) {
	c.model.ChatMode = mode
}

func (c *commandController) Usage() provider.Usage {
	return c.model.Usage
}

func (c *commandController) VisibleCommands() []command.Definition {
	return c.model.Commands.Visible()
}

func (c *commandController) ShowLocalMessage(message string) {
	c.model.showLocalMessage(message)
}

func (c *commandController) SendUserMessage(ctx context.Context, message string) error {
	next, cmd := c.model.startUserMessage(message)
	*c.model = next
	c.cmd = cmd
	return nil
}

func (c *commandController) RunSkill(ctx context.Context, name, args string) error {
	runCtx, cancel := context.WithCancel(context.Background())
	events, err := c.model.Runner.SubmitSkill(runCtx, name, args)
	if err != nil {
		cancel()
		return err
	}
	c.model.StreamCancel = cancel
	c.model.events = events
	c.model.Current = UIMessage{Role: provider.RoleAssistant, Status: MessageStatusStreaming}
	c.model.Status = StatusStreaming
	input := "/" + name
	if strings.TrimSpace(args) != "" {
		input += " " + strings.TrimSpace(args)
	}
	c.model.Output = append(c.model.Output, userBlock(input))
	c.cmd = waitForEvent(events)
	return nil
}

func (c *commandController) Compact(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	events, err := c.model.Runner.Compact(runCtx)
	if err != nil {
		cancel()
		return err
	}
	c.model.StreamCancel = cancel
	c.model.events = events
	c.model.Current = UIMessage{Role: provider.RoleAssistant, Status: MessageStatusStreaming}
	c.model.Status = StatusStreaming
	c.cmd = waitForEvent(events)
	return nil
}

func (c *commandController) ClearAndResetSession(ctx context.Context) error {
	if err := c.model.Runner.ResetSession(ctx); err != nil {
		return err
	}
	c.model.Output = nil
	c.model.Current = UIMessage{}
	c.model.Progress = ""
	c.model.LastError = ""
	c.model.CurrentTool = ""
	c.model.Usage = provider.Usage{}
	return nil
}

func (c *commandController) SessionStatus(ctx context.Context) (command.SessionStatus, error) {
	status := c.model.Runner.Status()
	summaries, err := c.model.Runner.SessionList()
	if err != nil {
		return command.SessionStatus{}, err
	}
	return command.SessionStatus{
		ID:           status.ID,
		Path:         status.Path,
		MessageCount: status.MessageCount,
		HasPlan:      status.HasPlan,
		Sessions:     convertSessionSummaries(summaries),
	}, nil
}

func (c *commandController) MemoryStatus(ctx context.Context) command.MemoryStatus {
	return c.model.Runner.MemoryStatus()
}

func (c *commandController) PermissionStatus(ctx context.Context) command.PermissionStatus {
	status := c.model.Runner.PermissionStatus()
	if c.model.ActivePermission != nil {
		status.ActivePrompt = true
		status.ActiveToolName = c.model.ActivePermission.Tool
	}
	status.QueuedPrompts = len(c.model.PermissionQueue)
	return status
}

func (c *commandController) ListCatalogSkills() []command.SkillSummary {
	if err := c.model.Runner.ReloadSkillCommands(context.Background(), c.model.Commands); err != nil {
		c.model.showLocalError(err.Error())
	}
	return c.model.Runner.ListCatalogSkills()
}

func (c *commandController) AppStatus(ctx context.Context) command.StatusSnapshot {
	session := c.model.Runner.Status()
	return command.StatusSnapshot{
		Active:      c.model.Active,
		Model:       c.model.ProviderCfg.Model,
		AgentState:  c.model.agentState(),
		Mode:        c.model.ChatMode,
		Usage:       c.model.Usage,
		Progress:    c.model.Progress,
		LastError:   c.model.LastError,
		CurrentTool: c.model.CurrentTool,
		SessionID:   session.ID,
	}
}

func (c *commandController) Shutdown(ctx context.Context) error {
	if c.model.StreamCancel != nil {
		c.model.StreamCancel()
		c.model.StreamCancel = nil
	}
	c.model.denyAllPermissions()
	if c.model.RootCancel != nil {
		c.model.RootCancel()
	}
	if err := c.model.Runner.Close(); err != nil {
		return err
	}
	c.cmd = tea.Quit
	return nil
}

func convertSessionSummaries(summaries []sessionstore.Summary) []command.SessionSummary {
	out := make([]command.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		updated := ""
		if !summary.UpdatedAt.IsZero() {
			updated = summary.UpdatedAt.Format(time.RFC3339)
		}
		out = append(out, command.SessionSummary{
			ID:               summary.ID,
			Title:            summary.Title,
			MessageCount:     summary.MessageCount,
			UpdatedAt:        updated,
			CorruptLineCount: summary.CorruptLineCount,
		})
	}
	return out
}

func (m Model) handleCompletionKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyUp:
		m.moveCompletion(-1)
		return true, m, nil
	case keyDown:
		m.moveCompletion(1)
		return true, m, nil
	case keyTab:
		if item, ok := m.highlightedCompletion(); ok {
			m.textarea.SetValue(item.Canonical)
			m.textarea.CursorEnd()
			m.refreshCompletion()
		}
		return true, m, nil
	case keyEsc:
		m.Completion = command.CompletionState{}
		return true, m, nil
	}
	return false, m, nil
}

func (m Model) acceptCompletion() (tea.Model, tea.Cmd) {
	if m.Completion.NoMatch {
		return m.startSubmit()
	}
	item, ok := m.highlightedCompletion()
	if !ok {
		return m.startSubmit()
	}
	parsed := m.Commands.Parse(item.Canonical)
	if parsed.Command == nil {
		m.showLocalError(fmt.Sprintf("Unknown command %q. Use /help to see available commands.", item.Canonical))
		m.textarea.Reset()
		m.refreshCompletion()
		return m, nil
	}
	return m.dispatchCommand(*parsed.Command)
}

func (m *Model) refreshCompletion() {
	value := m.textarea.Value()
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "\n") {
		m.Completion = command.CompletionState{}
		return
	}
	result := m.Commands.Complete(value)
	highlighted := m.Completion.Highlighted
	if highlighted < 0 {
		highlighted = 0
	}
	if highlighted >= len(result.Items) {
		highlighted = len(result.Items) - 1
	}
	if highlighted < 0 {
		highlighted = 0
	}
	m.Completion = command.CompletionState{
		Active:      true,
		Query:       value,
		Items:       result.Items,
		Highlighted: highlighted,
		NoMatch:     result.NoMatch,
	}
}

func (m *Model) moveCompletion(delta int) {
	if len(m.Completion.Items) == 0 {
		return
	}
	n := len(m.Completion.Items)
	m.Completion.Highlighted = (m.Completion.Highlighted + delta + n) % n
}

func (m Model) highlightedCompletion() (command.CompletionItem, bool) {
	if len(m.Completion.Items) == 0 || m.Completion.NoMatch {
		return command.CompletionItem{}, false
	}
	idx := m.Completion.Highlighted
	if idx < 0 || idx >= len(m.Completion.Items) {
		idx = 0
	}
	return m.Completion.Items[idx], true
}

func (m Model) startRetry() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	events, err := m.Runner.Retry(ctx)
	if err != nil {
		cancel()
		m.Status = StatusError
		m.LastError = err.Error()
		m.Current.ErrorText = err.Error()
		return m, nil
	}
	m.StreamCancel = cancel
	m.events = events
	m.Current = UIMessage{Role: provider.RoleAssistant, Status: MessageStatusStreaming}
	m.Status = StatusStreaming
	return m, waitForEvent(events)
}

func (m Model) handleStream(event provider.StreamEvent) (tea.Model, tea.Cmd) {
	switch event.Type {
	case provider.StreamEventTypeTextDelta:
		m.Current.Content += event.Delta
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeThinkingDelta:
		m.Current.Thinking += event.Delta
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeUsage:
		m.Usage = event.Usage
		m.Current.Usage = event.Usage
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeProgress:
		if event.Progress != nil {
			m.Progress = event.Progress.Message
			if event.Progress.Iteration > 0 && event.Progress.MaxIteration > 0 {
				m.Progress = event.Progress.Phase + fmt.Sprintf(" %d/%d", event.Progress.Iteration, event.Progress.MaxIteration)
			}
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeContext:
		if event.Context != nil {
			message := contextStatusLine(*event.Context)
			m.Progress = message
			m.Output = append(m.Output, message)
			return m, waitForEvent(m.events)
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeToolCallStart:
		if event.ToolCall != nil {
			m.CurrentTool = event.ToolCall.Name
			m.Output = append(m.Output, toolCallLine(*event.ToolCall))
			return m, waitForEvent(m.events)
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeToolCallDone:
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeToolResult:
		if event.ToolResult != nil {
			m.Output = append(m.Output, toolResultSummary(*event.ToolResult))
			return m, waitForEvent(m.events)
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypePermissionRequest:
		if event.Permission != nil {
			m.enqueuePermission(*event.Permission)
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeError:
		m.Status = StatusError
		m.LastError = event.ErrorText
		m.Current.ErrorText = event.ErrorText
		m.Current.Status = MessageStatusError
		m.StreamCancel = nil
		m.Output = append(m.Output, errorBlock(event.ErrorText))
		return m, nil
	case provider.StreamEventTypeCancelled:
		m.Status = StatusIdle
		m.StreamCancel = nil
		m.Current.Status = MessageStatusDone
		if strings.TrimSpace(m.Current.Content) != "" {
			m.Output = append(m.Output, m.Current.Content)
		}
		m.Current = UIMessage{}
		return m, nil
	case provider.StreamEventTypeDone:
		m.Status = StatusIdle
		m.StreamCancel = nil
		m.Current.Status = MessageStatusDone
		content := m.Current.Content
		if strings.TrimSpace(content) != "" {
			m.Runner.CommitAssistant(content)
		}
		rendered := content
		if m.Renderer != nil {
			if out, err := m.Renderer.Render(content, m.Width); err == nil {
				rendered = out
			}
		}
		m.Current = UIMessage{}
		if strings.TrimSpace(rendered) != "" {
			m.Output = append(m.Output, rendered)
		}
		return m, nil
	}
	return m, nil
}

func contextStatusLine(event provider.ContextEvent) string {
	if event.ErrorText != "" {
		return fmt.Sprintf("%s failed: %s", event.Message, event.ErrorText)
	}
	if event.BeforeTokens > 0 || event.AfterTokens > 0 {
		return fmt.Sprintf("%s, tokens %d -> %d", event.Message, event.BeforeTokens, event.AfterTokens)
	}
	if event.ReplacedToolResults > 0 {
		return fmt.Sprintf("%s, offloaded %d tool results", event.Message, event.ReplacedToolResults)
	}
	return event.Message
}

func (m Model) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var grant permission.UserGrant
	switch strings.ToLower(msg.String()) {
	case "o":
		grant = permission.GrantOnce
	case "s":
		grant = permission.GrantSession
	case "p":
		grant = permission.GrantPermanent
	case "n":
		grant = permission.GrantDeny
	default:
		return m, nil
	}
	m.respondPermission(grant)
	return m, nil
}

func (m *Model) enqueuePermission(prompt permission.Prompt) {
	if prompt.Response == nil {
		prompt.Response = make(chan permission.UserGrant, 1)
	}
	m.PermissionQueue = append(m.PermissionQueue, prompt)
	m.advancePermission()
}

func (m *Model) advancePermission() {
	if m.ActivePermission != nil || len(m.PermissionQueue) == 0 {
		return
	}
	next := m.PermissionQueue[0]
	m.PermissionQueue = m.PermissionQueue[1:]
	m.ActivePermission = &next
}

func (m *Model) respondPermission(grant permission.UserGrant) {
	if m.ActivePermission == nil {
		return
	}
	select {
	case m.ActivePermission.Response <- grant:
	default:
	}
	m.ActivePermission = nil
	m.advancePermission()
}

func (m *Model) denyAllPermissions() {
	if m.ActivePermission != nil {
		select {
		case m.ActivePermission.Response <- permission.GrantDeny:
		default:
		}
		m.ActivePermission = nil
	}
	for _, prompt := range m.PermissionQueue {
		if prompt.Response != nil {
			select {
			case prompt.Response <- permission.GrantDeny:
			default:
			}
		}
	}
	m.PermissionQueue = nil
}
