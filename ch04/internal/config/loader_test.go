package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewcode/internal/permission"
)

func TestLoadWithProjectOverride(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	mustMkdir(t, filepath.Join(home, ".mewcode"))
	mustMkdir(t, project)

	write(t, filepath.Join(home, ".mewcode", "config.yaml"), `
active: openai
providers:
  openai:
    protocol: openai
    model: gpt-global
    base_url: https://api.openai.com/v1
    api_key: sk-test-secret-value
  deepseek:
    protocol: openai
    model: deepseek-chat
    base_url: https://api.deepseek.com
    api_key: sk-deepseek
`)
	write(t, filepath.Join(project, "mewcode.yaml"), `
active: deepseek
providers:
  openai:
    model: gpt-project
`)

	cfg, err := LoadWithOptions(LoadOptions{HomeDir: home, ProjectDir: project})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if cfg.Active != "deepseek" {
		t.Fatalf("active = %q, want deepseek", cfg.Active)
	}
	if cfg.Providers["openai"].Model != "gpt-project" {
		t.Fatalf("project model override failed: %q", cfg.Providers["openai"].Model)
	}
}

func TestInvalidProtocolDoesNotLeakAPIKey(t *testing.T) {
	cfg := AppConfig{
		Active: "bad",
		Providers: map[string]ProviderConfig{
			"bad": {
				Protocol: "deepseek",
				Model:    "x",
				BaseURL:  "https://example.com",
				APIKey:   "sk-test-secret-value",
			},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() nil error, want invalid protocol error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %q, want unsupported protocol", err.Error())
	}
	if strings.Contains(err.Error(), "sk-test-secret-value") {
		t.Fatalf("error leaked api key: %q", err.Error())
	}
}

func TestLoadExpandsEnvironmentVariables(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	mustMkdir(t, home)
	mustMkdir(t, project)
	t.Setenv("MEWCODE_TEST_API_KEY", "sk-from-env")

	write(t, filepath.Join(project, "mewcode.yaml"), `
active: openai
providers:
  openai:
    protocol: openai
    model: gpt
    base_url: https://api.openai.com/v1
    api_key: ${MEWCODE_TEST_API_KEY}
`)

	cfg, err := LoadWithOptions(LoadOptions{HomeDir: home, ProjectDir: project})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}
	if got := cfg.Providers["openai"].APIKey; got != "sk-from-env" {
		t.Fatalf("api_key = %q, want environment value", got)
	}
}

func TestLoadDetailedIncludesLocalConfigAndPermissionLayers(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	mustMkdir(t, filepath.Join(home, ".mewcode"))
	mustMkdir(t, project)
	write(t, filepath.Join(home, ".mewcode", "config.yaml"), `
active: openai
permissions:
  rules:
    - action: allow
      pattern: Read(*)
providers:
  openai:
    protocol: openai
    model: gpt-global
    base_url: https://api.openai.com/v1
    api_key: sk-global
`)
	write(t, filepath.Join(project, "mewcode.yaml"), `
permissions:
  rules:
    - action: allow
      pattern: Bash(go test *)
providers:
  openai:
    model: gpt-project
`)
	write(t, filepath.Join(project, "mewcode.local.yaml"), `
permissions:
  mode: acceptEdits
  rules:
    - action: deny
      pattern: Bash(rm *)
providers:
  openai:
    model: gpt-local
`)
	loaded, err := LoadDetailedWithOptions(LoadOptions{HomeDir: home, ProjectDir: project})
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Config.Providers["openai"].Model; got != "gpt-local" {
		t.Fatalf("model = %q, want local override", got)
	}
	if loaded.Config.Permissions.Mode != permission.ModeAcceptEdits {
		t.Fatalf("mode = %q, want acceptEdits", loaded.Config.Permissions.Mode)
	}
	if len(loaded.PermissionLayers) != 3 {
		t.Fatalf("permission layers = %d, want 3", len(loaded.PermissionLayers))
	}
	if loaded.PermissionLayers[2].Source.Kind != permission.SourceLocal {
		t.Fatalf("last layer = %s, want local", loaded.PermissionLayers[2].Source.Kind)
	}
}

func TestInactiveProviderCanHaveMissingAPIKey(t *testing.T) {
	cfg := AppConfig{
		Active: "ark",
		Providers: map[string]ProviderConfig{
			"ark": {
				Protocol: ProtocolOpenAI,
				Model:    "doubao",
				BaseURL:  "https://ark.example.com/api/v3",
				APIKey:   "ark-key",
			},
			"openai": {
				Protocol: ProtocolOpenAI,
				Model:    "gpt-4o",
				BaseURL:  "https://api.openai.com/v1",
			},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMissingActive(t *testing.T) {
	err := Validate(AppConfig{
		Active: "missing",
		Providers: map[string]ProviderConfig{
			"openai": {
				Protocol: ProtocolOpenAI,
				Model:    "gpt",
				BaseURL:  "https://example.com",
				APIKey:   "secret",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Validate() error = %v, want missing active", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
