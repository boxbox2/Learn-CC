package permission

import (
	"context"
	"encoding/json"

	"mewcode/internal/tool"
)

type Mode string

const (
	ModeDefault           Mode = "default"
	ModeAcceptEdits       Mode = "acceptEdits"
	ModePlan              Mode = "plan"
	ModeBypassPermissions Mode = "bypassPermissions"
)

type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

type DecisionStatus string

const (
	DecisionAllow DecisionStatus = "allow"
	DecisionDeny  DecisionStatus = "deny"
	DecisionAsk   DecisionStatus = "ask"
)

type SourceKind string

const (
	SourceUser      SourceKind = "user"
	SourceProject   SourceKind = "project"
	SourceLocal     SourceKind = "local"
	SourceSession   SourceKind = "session"
	SourceMode      SourceKind = "mode"
	SourceHardBlock SourceKind = "hardblock"
)

type Rule struct {
	Action  Action `yaml:"action"`
	Pattern string `yaml:"pattern"`
	Source  Source `yaml:"-"`
}

type Config struct {
	Mode  Mode   `yaml:"mode"`
	Rules []Rule `yaml:"rules"`
}

type Source struct {
	Kind     SourceKind
	Path     string
	Priority int
}

type Layer struct {
	Source Source
	Rules  []Rule
}

type Request struct {
	CallID     string
	Tool       string
	Arguments  json.RawMessage
	Fields     map[string]string
	Safety     tool.Safety
	WorkingDir string
	PathPolicy tool.PathPolicy
}

type Decision struct {
	Status      DecisionStatus
	Reason      string
	Source      SourceKind
	Rule        *Rule
	Suggestions []string
}

func (d Decision) IsHardBlock() bool {
	return d.Source == SourceHardBlock
}

type UserGrant string

const (
	GrantOnce      UserGrant = "once"
	GrantSession   UserGrant = "session"
	GrantPermanent UserGrant = "permanent"
	GrantDeny      UserGrant = "deny"
)

type Prompt struct {
	ID       string
	Tool     string
	Summary  string
	Reason   string
	Options  []UserGrant
	Response chan UserGrant
}

type Authorizer interface {
	Authorize(ctx context.Context, req Request) Decision
}

type Prompter interface {
	Confirm(ctx context.Context, prompt Prompt) UserGrant
}

type RuleStore interface {
	AppendAllowRule(pattern string) error
}
