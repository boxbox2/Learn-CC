package provider_test

import (
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	_ "mewcode/internal/provider/anthropic"
	_ "mewcode/internal/provider/openai"
)

func TestFactoryCreatesKnownProtocols(t *testing.T) {
	factory := provider.NewFactory()
	cases := []config.ProviderConfig{
		{Protocol: config.ProtocolOpenAI, Model: "gpt", BaseURL: "https://example.com", APIKey: "secret"},
		{Protocol: config.ProtocolAnthropic, Model: "claude", BaseURL: "https://example.com", APIKey: "secret"},
	}
	for _, tc := range cases {
		got, err := factory.Create("test", tc)
		if err != nil {
			t.Fatalf("Create(%s) error = %v", tc.Protocol, err)
		}
		if got == nil {
			t.Fatalf("Create(%s) returned nil", tc.Protocol)
		}
	}
}

func TestFactoryRejectsUnknownProtocol(t *testing.T) {
	_, err := provider.NewFactory().Create("bad", config.ProviderConfig{Protocol: "bad"})
	if err == nil {
		t.Fatal("Create() nil error, want unsupported protocol")
	}
}
