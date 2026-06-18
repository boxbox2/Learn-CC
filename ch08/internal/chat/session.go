package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"mewcode/internal/agent"
	"mewcode/internal/command"
	"mewcode/internal/config"
	"mewcode/internal/contextmgr"
	"mewcode/internal/memory"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/sessionstore"
	"mewcode/internal/skill"
	"mewcode/internal/tool"
)

type Session struct {
	provider    provider.Provider
	cfg         config.ProviderConfig
	tools       *tool.Registry
	workDir     string
	limits      tool.Limits
	paths       tool.PathPolicy
	agent       *agent.Runner
	contextMgr  *contextmgr.Manager
	archive     *sessionstore.Writer
	memory      *memory.Manager
	permissions *permission.Manager
	skills      *skill.Catalog
	active      *skill.ActiveStore
	skillExec   *skill.Executor
	runMu       sync.Mutex

	History     []provider.ChatMessage
	LastPlan    string
	LastRequest *agent.RunRequest

	lastCommitted string
}

func NewSession(p provider.Provider, cfg config.ProviderConfig) *Session {
	return NewSessionWithOptions(p, cfg, SessionOptions{Limits: tool.DefaultLimits()})
}

type SessionOptions struct {
	Tools          *tool.Registry
	WorkingDir     string
	Limits         tool.Limits
	PathPolicy     tool.PathPolicy
	Agent          *agent.Runner
	Context        *contextmgr.Manager
	Archive        *sessionstore.Writer
	Memory         *memory.Manager
	PromptCtx      agent.PromptContextProvider
	Skills         *skill.Catalog
	ActiveSkills   *skill.ActiveStore
	SkillExecutor  *skill.Executor
	InitialHistory []provider.ChatMessage
	LastRestoredAt time.Time
	Now            func() time.Time
	Permissions    *permission.Manager
}

func NewSessionWithOptions(p provider.Provider, cfg config.ProviderConfig, opts SessionOptions) *Session {
	limits := opts.Limits
	if limits == (tool.Limits{}) {
		limits = tool.DefaultLimits()
	}
	paths := opts.PathPolicy
	if paths.Root == "" {
		paths.Root = opts.WorkingDir
	}
	activeSkills := opts.ActiveSkills
	if activeSkills == nil {
		activeSkills = skill.NewActiveStore()
	}
	promptCtx := sessionPromptContext{
		Base:   opts.PromptCtx,
		Skills: opts.Skills,
		Active: activeSkills,
	}
	s := &Session{
		provider:    p,
		cfg:         cfg,
		tools:       opts.Tools,
		workDir:     opts.WorkingDir,
		limits:      limits,
		paths:       paths,
		agent:       opts.Agent,
		contextMgr:  opts.Context,
		archive:     opts.Archive,
		memory:      opts.Memory,
		permissions: opts.Permissions,
		skills:      opts.Skills,
		active:      activeSkills,
		skillExec:   opts.SkillExecutor,
		History:     initialHistory(opts),
	}
	if s.skillExec == nil {
		s.skillExec = &skill.Executor{
			Catalog:     opts.Skills,
			Active:      activeSkills,
			Tools:       opts.Tools,
			Provider:    p,
			Config:      cfg,
			WorkDir:     opts.WorkingDir,
			Limits:      limits,
			Paths:       paths,
			Context:     opts.Context,
			Permissions: opts.Permissions,
		}
	}
	if s.agent == nil {
		s.agent = &agent.Runner{
			Provider:          p,
			Config:            cfg,
			Tools:             opts.Tools,
			WorkDir:           opts.WorkingDir,
			Limits:            limits,
			Paths:             paths,
			PermissionManager: opts.Permissions,
			ContextManager:    opts.Context,
			PromptContext:     promptCtx,
			Recorder:          opts.Archive,
		}
	} else if opts.Context != nil {
		s.agent.ContextManager = opts.Context
	}
	if opts.Agent != nil {
		s.agent.PromptContext = promptCtx
		if opts.Archive != nil {
			s.agent.Recorder = opts.Archive
		}
	}
	return s
}

type sessionPromptContext struct {
	Base   agent.PromptContextProvider
	Skills *skill.Catalog
	Active *skill.ActiveStore
}

func (c sessionPromptContext) CustomInstructions(ctx context.Context) string {
	if c.Base == nil {
		return ""
	}
	return c.Base.CustomInstructions(ctx)
}

func (c sessionPromptContext) LongTermMemory(ctx context.Context) string {
	if c.Base == nil {
		return ""
	}
	return c.Base.LongTermMemory(ctx)
}

func (c sessionPromptContext) SkillsCatalog(ctx context.Context) string {
	if c.Skills == nil {
		return ""
	}
	return c.Skills.PromptCatalog()
}

func (c sessionPromptContext) ActiveSkills(ctx context.Context) string {
	if c.Active == nil {
		return ""
	}
	return c.Active.PromptText()
}

func initialHistory(opts SessionOptions) []provider.ChatMessage {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	return appendStaleRestoreNote(opts.InitialHistory, opts.LastRestoredAt, now)
}

type SubmitMode string

const (
	SubmitModeDefault SubmitMode = "default"
	SubmitModePlan    SubmitMode = "plan"
)

type SessionStatus struct {
	ID           string
	Path         string
	MessageCount int
	HasPlan      bool
}

func (s *Session) Submit(ctx context.Context, input string, mode SubmitMode) (<-chan provider.StreamEvent, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("input is required")
	}
	switch mode {
	case SubmitModePlan:
		return s.submitPlan(ctx, input)
	default:
		return s.submitChat(ctx, input)
	}
}

func (s *Session) Retry(ctx context.Context) (<-chan provider.StreamEvent, error) {
	if s.LastRequest == nil {
		return nil, fmt.Errorf("no request to retry")
	}
	req := *s.LastRequest
	req.Messages = append([]provider.ChatMessage(nil), s.LastRequest.Messages...)
	savePlan := ""
	if req.Mode == agent.RunModePlan {
		savePlan = "plan"
	}
	return s.run(ctx, req, savePlan)
}

func (s *Session) CommitAssistant(content string) {
	content = strings.TrimSpace(content)
	if content == "" || content == s.lastCommitted {
		return
	}
	s.History = append(s.History, provider.ChatMessage{Role: provider.RoleAssistant, Content: content})
	s.lastCommitted = content
}

func (s *Session) submitChat(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	s.LastPlan = ""
	messages := append([]provider.ChatMessage(nil), s.History...)
	userMsg := provider.ChatMessage{Role: provider.RoleUser, Content: input}
	if err := s.record(userMsg); err != nil {
		return nil, err
	}
	messages = append(messages, userMsg)
	req := agent.RunRequest{Messages: messages, Mode: agent.RunModeExecute, Tools: agent.ToolSet{Mode: agent.ToolSetAll, Overlay: s.active}}
	return s.run(ctx, req, "")
}

func (s *Session) submitPlan(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	messages := append([]provider.ChatMessage(nil), s.History...)
	userMsg := provider.ChatMessage{Role: provider.RoleUser, Content: input}
	if err := s.record(userMsg); err != nil {
		return nil, err
	}
	messages = append(messages, userMsg)
	req := agent.RunRequest{Messages: messages, Mode: agent.RunModePlan, Tools: agent.ToolSet{Mode: agent.ToolSetReadOnly, Overlay: s.active}}
	return s.run(ctx, req, "plan")
}

func (s *Session) SubmitSkill(ctx context.Context, name, args string) (<-chan provider.StreamEvent, error) {
	if s.skillExec == nil {
		return nil, fmt.Errorf("skill executor is not configured")
	}
	def, err := s.skillExec.Definition(name)
	if err != nil {
		return nil, err
	}
	if err := s.skillExec.Activate(def); err != nil {
		return nil, err
	}
	rendered := skill.RenderExecutionPrompt(def, args)
	if def.Metadata.Mode == skill.ModeFork {
		return s.submitForkSkill(ctx, def, rendered)
	}
	return s.submitInlineSkill(ctx, def, rendered)
}

func (s *Session) submitInlineSkill(ctx context.Context, def skill.Definition, rendered string) (<-chan provider.StreamEvent, error) {
	s.LastPlan = ""
	messages := append([]provider.ChatMessage(nil), s.History...)
	userMsg := provider.ChatMessage{Role: provider.RoleUser, Content: rendered}
	if err := s.record(userMsg); err != nil {
		return nil, err
	}
	messages = append(messages, userMsg)
	req := agent.RunRequest{
		Messages: messages,
		Mode:     agent.RunModeExecute,
		Tools: agent.ToolSet{
			Mode:    agent.ToolSetAll,
			Names:   def.Metadata.AllowedTools,
			Overlay: s.active,
		},
		Model: strings.TrimSpace(def.Metadata.Model),
	}
	return s.run(ctx, req, "")
}

func (s *Session) submitForkSkill(ctx context.Context, def skill.Definition, rendered string) (<-chan provider.StreamEvent, error) {
	s.runMu.Lock()
	out := make(chan provider.StreamEvent)
	go func() {
		defer s.runMu.Unlock()
		defer close(out)
		result, err := s.skillExec.RunFork(ctx, def, rendered, s.History)
		if err != nil {
			text := fmt.Sprintf("[skill %s failed: %s]", def.Metadata.Name, err.Error())
			msg := provider.ChatMessage{Role: provider.RoleAssistant, Content: text}
			s.History = append(s.History, msg)
			if recordErr := s.record(msg); recordErr != nil {
				out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: recordErr.Error()}
				return
			}
			out <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: text}
			out <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
			return
		}
		text := strings.TrimSpace(result.Text)
		if text != "" {
			msg := provider.ChatMessage{Role: provider.RoleAssistant, Content: text}
			s.History = append(s.History, msg)
			if err := s.record(msg); err != nil {
				out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
				return
			}
			out <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: text}
		}
		if s.contextMgr != nil {
			s.contextMgr.RecordUsage(result.Usage, len(s.History))
		}
		s.maybeUpdateMemory(result)
		out <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	}()
	return out, nil
}

func (s *Session) run(ctx context.Context, req agent.RunRequest, savePlan string) (<-chan provider.StreamEvent, error) {
	s.runMu.Lock()
	run, err := s.agent.Run(ctx, req)
	if err != nil {
		s.runMu.Unlock()
		return nil, err
	}
	s.LastRequest = &agent.RunRequest{
		Messages:    append([]provider.ChatMessage(nil), req.Messages...),
		Mode:        req.Mode,
		Tools:       req.Tools,
		PlanContext: req.PlanContext,
		Model:       req.Model,
	}
	out := make(chan provider.StreamEvent)
	go func() {
		defer s.runMu.Unlock()
		defer close(out)
		var terminal *provider.StreamEvent
		for event := range run.Events {
			switch event.Type {
			case provider.StreamEventTypeDone, provider.StreamEventTypeCancelled, provider.StreamEventTypeError:
				copy := event
				terminal = &copy
			default:
				out <- event
			}
		}
		result, ok := <-run.Done
		if !ok {
			return
		}
		if len(result.Messages) > 0 {
			s.History = append([]provider.ChatMessage(nil), result.Messages...)
		}
		if result.Stop == agent.StopFinal {
			s.lastCommitted = strings.TrimSpace(result.Text)
			if savePlan == "plan" {
				s.LastPlan = strings.TrimSpace(result.Text)
			}
			s.maybeUpdateMemory(result)
		}
		if terminal != nil {
			out <- *terminal
		}
	}()
	return out, nil
}

func (s *Session) record(msg provider.ChatMessage) error {
	if s.archive == nil {
		return nil
	}
	return s.archive.Append(msg)
}

func (s *Session) maybeUpdateMemory(result agent.Result) {
	if s.memory == nil || len(s.History) == 0 {
		return
	}
	last := s.History[len(s.History)-1]
	if last.Role != provider.RoleAssistant || len(last.ToolCalls) > 0 {
		return
	}
	s.memory.UpdateAsync(memory.Snapshot{
		SessionID: s.sessionID(),
		Messages:  cloneMessages(s.History),
		FinalText: result.Text,
		CreatedAt: time.Now(),
	})
}

func (s *Session) sessionID() string {
	if s.archive != nil {
		return s.archive.ID()
	}
	return ""
}

func (s *Session) Status() SessionStatus {
	return SessionStatus{
		ID:           s.sessionID(),
		Path:         s.sessionPath(),
		MessageCount: len(s.History),
		HasPlan:      strings.TrimSpace(s.LastPlan) != "",
	}
}

func (s *Session) SessionList() ([]sessionstore.Summary, error) {
	if s.workDir == "" {
		return nil, fmt.Errorf("working directory is not configured")
	}
	return sessionstore.Scan(s.workDir)
}

func (s *Session) MemoryStatus() command.MemoryStatus {
	if s == nil || s.memory == nil {
		return command.MemoryStatus{}
	}
	lastError := ""
	if s.memory.LastError != nil {
		lastError = s.memory.LastError.Error()
	}
	return command.MemoryStatus{
		UserAvailable:    s.memory.User != nil,
		ProjectAvailable: s.memory.Project != nil,
		LastError:        lastError,
	}
}

func (s *Session) PermissionStatus() command.PermissionStatus {
	if s == nil || s.permissions == nil {
		return command.PermissionStatus{}
	}
	return command.PermissionStatus{Mode: string(s.permissions.Mode())}
}

func (s *Session) ListCatalogSkills() []command.SkillSummary {
	if s == nil || s.skills == nil {
		return nil
	}
	summaries := s.skills.Summaries()
	out := make([]command.SkillSummary, 0, len(summaries))
	active := map[string]bool{}
	if s.active != nil {
		for _, name := range s.active.List() {
			active[name] = true
		}
	}
	for _, summary := range summaries {
		out = append(out, command.SkillSummary{
			Name:        summary.Name,
			Description: summary.Description,
			Active:      active[summary.Name],
		})
	}
	return out
}

func (s *Session) ReloadSkillCommands(ctx context.Context, reg *command.Registry) error {
	if s == nil || s.skills == nil {
		return nil
	}
	if err := s.skills.Reload(ctx); err != nil {
		return err
	}
	if reg != nil {
		reg.RemoveSkillCommands()
		if err := skill.RegisterSkillCommands(reg, s.skills, nil); err != nil {
			return err
		}
		reg.MustValidate()
	}
	return nil
}

func (s *Session) ResetSession(ctx context.Context) error {
	if s.workDir == "" {
		return fmt.Errorf("working directory is not configured")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.archive != nil {
		if err := s.archive.Close(); err != nil {
			return err
		}
	}
	archive, err := sessionstore.Create(s.workDir)
	if err != nil {
		return err
	}
	s.archive = archive
	if s.agent != nil {
		s.agent.Recorder = archive
	}
	s.History = nil
	s.LastPlan = ""
	s.LastRequest = nil
	s.lastCommitted = ""
	if s.active != nil {
		s.active.Clear()
	}
	if s.contextMgr != nil && s.contextMgr.Session != nil {
		s.contextMgr.Session.ClearAnchor()
	}
	return nil
}

func (s *Session) Close() error {
	if s == nil || s.archive == nil {
		return nil
	}
	return s.archive.Close()
}

func (s *Session) sessionPath() string {
	if s.archive != nil {
		return s.archive.Path()
	}
	return ""
}

func cloneMessages(messages []provider.ChatMessage) []provider.ChatMessage {
	out := make([]provider.ChatMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].ToolCalls = append([]provider.ToolCall(nil), msg.ToolCalls...)
		out[i].ToolResults = append([]provider.ToolResultMessage(nil), msg.ToolResults...)
	}
	return out
}

func appendStaleRestoreNote(history []provider.ChatMessage, lastRestoredAt time.Time, now func() time.Time) []provider.ChatMessage {
	history = append([]provider.ChatMessage(nil), history...)
	if len(history) == 0 || lastRestoredAt.IsZero() {
		return history
	}
	if now == nil {
		now = time.Now
	}
	gap := now().Sub(lastRestoredAt)
	if gap > 24*time.Hour {
		history = append(history, provider.ChatMessage{Role: provider.RoleSystem, Content: fmt.Sprintf("This conversation was restored after %s of inactivity.", gap.Round(time.Second))})
	}
	return history
}

func (s *Session) Compact(ctx context.Context) (<-chan provider.StreamEvent, error) {
	if s.contextMgr == nil {
		return nil, fmt.Errorf("context manager is not configured")
	}
	s.runMu.Lock()
	out := make(chan provider.StreamEvent)
	go func() {
		defer s.runMu.Unlock()
		defer close(out)
		allowedTools := agent.AllowedDefinitions(s.tools, agent.ToolSet{Mode: agent.ToolSetAll, Overlay: s.active})
		out <- provider.StreamEvent{
			Type: provider.StreamEventTypeContext,
			Context: &provider.ContextEvent{
				Phase:   "summary",
				Mode:    string(contextmgr.ManageModeManual),
				Message: "compacting context",
			},
		}
		result, err := s.contextMgr.ForceCompact(ctx, s.History, allowedTools)
		if err != nil {
			out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
			return
		}
		s.History = append([]provider.ChatMessage(nil), result.Messages...)
		if s.archive != nil {
			if err := s.archive.Replace(s.History); err != nil {
				out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
				return
			}
		}
		out <- provider.StreamEvent{
			Type: provider.StreamEventTypeContext,
			Context: &provider.ContextEvent{
				Phase:        "summary",
				Mode:         string(contextmgr.ManageModeManual),
				Message:      "context compacted",
				BeforeTokens: result.Before.Tokens,
				AfterTokens:  result.After.Tokens,
			},
		}
		out <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	}()
	return out, nil
}
