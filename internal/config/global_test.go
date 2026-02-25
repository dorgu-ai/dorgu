package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalConfigDir(t *testing.T) {
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	defer func() {
		if originalXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", originalXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()

	os.Unsetenv("XDG_CONFIG_HOME")
	dir := GlobalConfigDir()

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "dorgu")
	if dir != expected {
		t.Errorf("GlobalConfigDir() = %q, want %q", dir, expected)
	}
}

func TestGlobalConfigDir_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	dir := GlobalConfigDir()
	expected := "/custom/config/dorgu"
	if dir != expected {
		t.Errorf("GlobalConfigDir() with XDG = %q, want %q", dir, expected)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	path := GlobalConfigPath()
	expected := "/custom/config/dorgu/config.yaml"
	if path != expected {
		t.Errorf("GlobalConfigPath() = %q, want %q", path, expected)
	}
}

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()

	if cfg.Version != "1" {
		t.Errorf("DefaultGlobalConfig().Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.LLM.Provider != "" {
		t.Errorf("DefaultGlobalConfig().LLM.Provider = %q, want empty", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKey != "" {
		t.Errorf("DefaultGlobalConfig().LLM.APIKey = %q, want empty", cfg.LLM.APIKey)
	}
	if cfg.LLM.Model != "" {
		t.Errorf("DefaultGlobalConfig().LLM.Model = %q, want empty", cfg.LLM.Model)
	}
	if cfg.Defaults.Namespace != "default" {
		t.Errorf("DefaultGlobalConfig().Defaults.Namespace = %q, want %q", cfg.Defaults.Namespace, "default")
	}
	if cfg.Defaults.Registry != "" {
		t.Errorf("DefaultGlobalConfig().Defaults.Registry = %q, want empty", cfg.Defaults.Registry)
	}
	if cfg.Defaults.OrgName != "" {
		t.Errorf("DefaultGlobalConfig().Defaults.OrgName = %q, want empty", cfg.Defaults.OrgName)
	}
}

func TestGlobalConfig_Set(t *testing.T) {
	tests := []struct {
		key     string
		value   string
		checkFn func(*GlobalConfig) string
		wantErr bool
	}{
		{"llm.provider", "openai", func(c *GlobalConfig) string { return c.LLM.Provider }, false},
		{"llm.provider", "anthropic", func(c *GlobalConfig) string { return c.LLM.Provider }, false},
		{"llm.provider", "gemini", func(c *GlobalConfig) string { return c.LLM.Provider }, false},
		{"llm.provider", "ollama", func(c *GlobalConfig) string { return c.LLM.Provider }, false},
		{"llm.provider", "", func(c *GlobalConfig) string { return c.LLM.Provider }, false},
		{"llm.api_key", "sk-test123", func(c *GlobalConfig) string { return c.LLM.APIKey }, false},
		{"llm.model", "gpt-4-turbo", func(c *GlobalConfig) string { return c.LLM.Model }, false},
		{"defaults.namespace", "production", func(c *GlobalConfig) string { return c.Defaults.Namespace }, false},
		{"defaults.registry", "gcr.io/my-project", func(c *GlobalConfig) string { return c.Defaults.Registry }, false},
		{"defaults.org_name", "my-org", func(c *GlobalConfig) string { return c.Defaults.OrgName }, false},
	}

	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			cfg := DefaultGlobalConfig()
			err := cfg.Set(tt.key, tt.value)

			if (err != nil) != tt.wantErr {
				t.Errorf("Set(%q, %q) error = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkFn(cfg) != tt.value {
				t.Errorf("Set(%q, %q) result = %q, want %q", tt.key, tt.value, tt.checkFn(cfg), tt.value)
			}
		})
	}
}

func TestGlobalConfig_Set_InvalidProvider(t *testing.T) {
	cfg := DefaultGlobalConfig()
	err := cfg.Set("llm.provider", "invalid-provider")

	if err == nil {
		t.Error("Set(llm.provider, invalid-provider) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid LLM provider") {
		t.Errorf("Set() error = %q, want to contain 'invalid LLM provider'", err.Error())
	}
}

func TestGlobalConfig_Set_UnknownKey(t *testing.T) {
	cfg := DefaultGlobalConfig()
	err := cfg.Set("unknown.key", "value")

	if err == nil {
		t.Error("Set(unknown.key, value) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("Set() error = %q, want to contain 'unknown config key'", err.Error())
	}
}

func TestGlobalConfig_Get(t *testing.T) {
	cfg := &GlobalConfig{
		Version: "1",
		LLM: GlobalLLMConfig{
			Provider: "anthropic",
			APIKey:   "",
			Model:    "claude-3",
		},
		Defaults: GlobalDefaults{
			Namespace: "staging",
			Registry:  "docker.io/myorg",
			OrgName:   "myorg",
		},
	}

	tests := []struct {
		key     string
		want    string
		wantErr bool
	}{
		{"llm.provider", "anthropic", false},
		{"llm.api_key", "", false},
		{"llm.model", "claude-3", false},
		{"defaults.namespace", "staging", false},
		{"defaults.registry", "docker.io/myorg", false},
		{"defaults.org_name", "myorg", false},
		{"unknown.key", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := cfg.Get(tt.key)

			if (err != nil) != tt.wantErr {
				t.Errorf("Get(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestGlobalConfig_Get_MaskedAPIKey(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: GlobalLLMConfig{
			APIKey: "sk-1234567890abcdef",
		},
	}

	got, err := cfg.Get("llm.api_key")
	if err != nil {
		t.Fatalf("Get(llm.api_key) error = %v", err)
	}

	if !strings.Contains(got, "****") {
		t.Errorf("Get(llm.api_key) = %q, want masked key with ****", got)
	}
	if got == cfg.LLM.APIKey {
		t.Errorf("Get(llm.api_key) returned unmasked key")
	}
}

func TestGlobalConfig_GetAPIKey_EnvPriority(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: GlobalLLMConfig{
			APIKey: "config-key",
		},
	}

	t.Setenv("OPENAI_API_KEY", "env-openai-key")
	t.Setenv("ANTHROPIC_API_KEY", "env-anthropic-key")
	t.Setenv("GEMINI_API_KEY", "env-gemini-key")

	tests := []struct {
		provider string
		want     string
	}{
		{"openai", "env-openai-key"},
		{"anthropic", "env-anthropic-key"},
		{"gemini", "env-gemini-key"},
		{"ollama", "config-key"},
		{"unknown", "config-key"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := cfg.GetAPIKey(tt.provider)
			if got != tt.want {
				t.Errorf("GetAPIKey(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestGlobalConfig_GetAPIKey_GoogleAPIKeyFallback(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: GlobalLLMConfig{
			APIKey: "config-key",
		},
	}

	os.Unsetenv("GEMINI_API_KEY")
	t.Setenv("GOOGLE_API_KEY", "google-api-key")

	got := cfg.GetAPIKey("gemini")
	if got != "google-api-key" {
		t.Errorf("GetAPIKey(gemini) with GOOGLE_API_KEY = %q, want %q", got, "google-api-key")
	}
}

func TestGlobalConfig_GetEffectiveProvider(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: GlobalLLMConfig{
			Provider: "anthropic",
		},
	}

	tests := []struct {
		flagValue string
		want      string
	}{
		{"", "anthropic"},
		{"openai", "openai"},
		{"gemini", "gemini"},
	}

	for _, tt := range tests {
		t.Run("flag="+tt.flagValue, func(t *testing.T) {
			got := cfg.GetEffectiveProvider(tt.flagValue)
			if got != tt.want {
				t.Errorf("GetEffectiveProvider(%q) = %q, want %q", tt.flagValue, got, tt.want)
			}
		})
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"", "(not set)"},
		{"abc", "***"},
		{"abcdefgh", "********"},
		{"sk-1234567890", "sk-1*****7890"},
		{"sk-1234567890abcdef", "sk-1***********cdef"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := maskKey(tt.key)
			if got != tt.want {
				t.Errorf("maskKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestEnvKeyForProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"gemini", "GEMINI_API_KEY"},
		{"ollama", ""},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := envKeyForProvider(tt.provider)
			if got != tt.want {
				t.Errorf("envKeyForProvider(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestGlobalConfig_ListAll(t *testing.T) {
	cfg := &GlobalConfig{
		LLM: GlobalLLMConfig{
			Provider: "openai",
			APIKey:   "sk-test123456789",
			Model:    "gpt-4",
		},
		Defaults: GlobalDefaults{
			Namespace: "production",
			Registry:  "gcr.io/project",
			OrgName:   "myorg",
		},
	}

	entries := cfg.ListAll()

	if len(entries) != 6 {
		t.Errorf("ListAll() returned %d entries, want 6", len(entries))
	}

	expectedKeys := []string{
		"llm.provider",
		"llm.api_key",
		"llm.model",
		"defaults.namespace",
		"defaults.registry",
		"defaults.org_name",
	}

	for i, key := range expectedKeys {
		if entries[i].Key != key {
			t.Errorf("ListAll()[%d].Key = %q, want %q", i, entries[i].Key, key)
		}
	}

	for _, entry := range entries {
		if entry.Key == "llm.api_key" {
			if entry.Value == cfg.LLM.APIKey {
				t.Error("ListAll() returned unmasked API key")
			}
		}
	}
}

func TestLoadGlobalConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("LoadGlobalConfig() returned non-default config")
	}
}

func TestLoadGlobalConfig_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "dorgu")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0600); err != nil {
		t.Fatalf("Failed to write empty config: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("LoadGlobalConfig() on empty file returned non-default config")
	}
}

func TestLoadGlobalConfig_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "dorgu")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	configContent := `version: "2"
llm:
  provider: anthropic
  model: claude-3
defaults:
  namespace: staging
`
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if cfg.Version != "2" {
		t.Errorf("LoadGlobalConfig().Version = %q, want %q", cfg.Version, "2")
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LoadGlobalConfig().LLM.Provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}
	if cfg.LLM.Model != "claude-3" {
		t.Errorf("LoadGlobalConfig().LLM.Model = %q, want %q", cfg.LLM.Model, "claude-3")
	}
	if cfg.Defaults.Namespace != "staging" {
		t.Errorf("LoadGlobalConfig().Defaults.Namespace = %q, want %q", cfg.Defaults.Namespace, "staging")
	}
}

func TestSaveGlobalConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg := &GlobalConfig{
		Version: "1",
		LLM: GlobalLLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
		},
		Defaults: GlobalDefaults{
			Namespace: "production",
		},
	}

	err := SaveGlobalConfig(cfg)
	if err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	configPath := filepath.Join(tmpDir, "dorgu", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("SaveGlobalConfig() did not create config file")
	}

	loaded, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() after save error = %v", err)
	}

	if loaded.LLM.Provider != cfg.LLM.Provider {
		t.Errorf("Loaded config LLM.Provider = %q, want %q", loaded.LLM.Provider, cfg.LLM.Provider)
	}
	if loaded.LLM.Model != cfg.LLM.Model {
		t.Errorf("Loaded config LLM.Model = %q, want %q", loaded.LLM.Model, cfg.LLM.Model)
	}
}
