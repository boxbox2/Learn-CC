package permission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewcode/internal/tool"
)

func TestRuleEnginePriorityAndDenyWins(t *testing.T) {
	req := Request{Tool: "Bash", Fields: map[string]string{"command": "go test ./..."}, Safety: tool.SafetySideEffect}
	engine := NewRuleEngine([]Layer{
		{Source: Source{Kind: SourceUser}, Rules: []Rule{{Action: ActionAllow, Pattern: "Bash(go *)"}}},
		{Source: Source{Kind: SourceProject}, Rules: []Rule{{Action: ActionAllow, Pattern: "Bash(go test *)"}}},
		{Source: Source{Kind: SourceLocal}, Rules: []Rule{
			{Action: ActionAllow, Pattern: "Bash(go test *)"},
			{Action: ActionDeny, Pattern: "Bash(go *)"},
		}},
	})
	decision := engine.Evaluate(req)
	if decision.Status != DecisionDeny || decision.Source != SourceLocal {
		t.Fatalf("decision = %#v, want local deny", decision)
	}
}

func TestRuleEngineFieldMatching(t *testing.T) {
	cases := []struct {
		rule Rule
		req  Request
	}{
		{Rule{Action: ActionAllow, Pattern: "Bash(git *)"}, Request{Tool: "Bash", Fields: map[string]string{"command": "git status"}}},
		{Rule{Action: ActionAllow, Pattern: "Write(src/*.go)"}, Request{Tool: "Write", Fields: map[string]string{"path": "src/main.go"}}},
		{Rule{Action: ActionAllow, Pattern: "Grep(pattern=TODO*)"}, Request{Tool: "Grep", Fields: map[string]string{"pattern": "TODO fix"}}},
	}
	for _, tc := range cases {
		engine := NewRuleEngine([]Layer{{Source: Source{Kind: SourceProject}, Rules: []Rule{tc.rule}}})
		if got := engine.Evaluate(tc.req); got.Status != DecisionAllow {
			t.Fatalf("Evaluate(%s) = %#v, want allow", tc.rule.Pattern, got)
		}
	}
}

func TestEvaluateMode(t *testing.T) {
	readReq := Request{Tool: "Read", Safety: tool.SafetyReadOnly}
	writeReq := Request{Tool: "Write", Safety: tool.SafetySideEffect}
	bashReq := Request{Tool: "Bash", Safety: tool.SafetySideEffect}
	if got := EvaluateMode(ModeDefault, readReq); got.Status != DecisionAllow {
		t.Fatalf("default read = %s", got.Status)
	}
	if got := EvaluateMode(ModeDefault, writeReq); got.Status != DecisionAsk {
		t.Fatalf("default write = %s", got.Status)
	}
	if got := EvaluateMode(ModeAcceptEdits, writeReq); got.Status != DecisionAllow {
		t.Fatalf("acceptEdits write = %s", got.Status)
	}
	if got := EvaluateMode(ModeAcceptEdits, bashReq); got.Status != DecisionAsk {
		t.Fatalf("acceptEdits bash = %s", got.Status)
	}
	if got := EvaluateMode(ModeBypassPermissions, bashReq); got.Status != DecisionAllow {
		t.Fatalf("bypass bash = %s", got.Status)
	}
}

func TestHardBlock(t *testing.T) {
	for _, command := range []string{"rm -rf", "rm -fr", "rm -rf /"} {
		danger := Request{Tool: "Bash", Fields: map[string]string{"command": command}}
		if got := CheckHardBlock(danger); got.Status != DecisionDeny || !got.IsHardBlock() {
			t.Fatalf("%q = %#v, want hard deny", command, got)
		}
	}
	normal := Request{Tool: "Bash", Fields: map[string]string{"command": "go test ./..."}}
	if got := CheckHardBlock(normal); got.Status == DecisionDeny {
		t.Fatalf("normal command hard denied: %#v", got)
	}
}

func TestManagerUserGrants(t *testing.T) {
	for _, grant := range []UserGrant{GrantOnce, GrantSession, GrantPermanent, GrantDeny} {
		t.Run(string(grant), func(t *testing.T) {
			store := &fakeStore{}
			manager := NewManager(ManagerOptions{
				Mode:     ModeDefault,
				Prompter: fakePrompter{grant: grant},
				Store:    store,
			})
			req := Request{Tool: "Write", Fields: map[string]string{"path": "a.go"}, Safety: tool.SafetySideEffect}
			decision := manager.Authorize(context.Background(), req)
			if grant == GrantDeny {
				if decision.Status != DecisionDeny || len(decision.Suggestions) == 0 {
					t.Fatalf("deny decision = %#v", decision)
				}
				return
			}
			if decision.Status != DecisionAllow {
				t.Fatalf("grant %s decision = %#v, want allow", grant, decision)
			}
			if grant == GrantPermanent && store.pattern == "" {
				t.Fatal("permanent grant did not write store")
			}
			if grant == GrantSession || grant == GrantPermanent {
				again := manager.Authorize(context.Background(), req)
				if again.Status != DecisionAllow {
					t.Fatalf("session rule decision = %#v, want allow", again)
				}
			}
		})
	}
}

func TestYAMLRuleStorePreservesExistingConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mewcode.local.yaml")
	if err := os.WriteFile(path, []byte("active: openai\nproviders:\n  openai:\n    model: gpt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := YAMLRuleStore{ProjectDir: root}
	if err := store.AppendAllowRule("Bash(git *)"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"active: openai", "providers:", "permissions:", "pattern: Bash(git *)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("written yaml missing %q:\n%s", want, text)
		}
	}
}

type fakePrompter struct {
	grant UserGrant
}

func (p fakePrompter) Confirm(context.Context, Prompt) UserGrant {
	return p.grant
}

type fakeStore struct {
	pattern string
}

func (s *fakeStore) AppendAllowRule(pattern string) error {
	s.pattern = pattern
	return nil
}
