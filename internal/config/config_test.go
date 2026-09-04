package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func TestDefaults(t *testing.T) {
	v := newViperWithDefaults(t)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Discord.ClientID != "" {
		t.Errorf("discord.client_id = %q, want %q", cfg.Discord.ClientID, "")
	}
	if !cfg.Discord.AutoReconnect {
		t.Error("discord.auto_reconnect = false, want true")
	}
	if cfg.Discord.ReconnectBaseInterval != 5*time.Second {
		t.Errorf("discord.reconnect_base_interval = %v, want 5s", cfg.Discord.ReconnectBaseInterval)
	}
	if cfg.Discord.MaxReconnectInterval != 60*time.Second {
		t.Errorf("discord.max_reconnect_interval = %v, want 60s", cfg.Discord.MaxReconnectInterval)
	}
	if cfg.Discord.CoalesceInterval != 5*time.Second {
		t.Errorf("discord.coalesce_interval = %v, want 5s", cfg.Discord.CoalesceInterval)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("server.host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8765 {
		t.Errorf("server.port = %d, want 8765", cfg.Server.Port)
	}
	if cfg.Server.WsPath != "/ws" {
		t.Errorf("server.ws_path = %q, want %q", cfg.Server.WsPath, "/ws")
	}
	if cfg.Server.ReadTimeout != 10*time.Second {
		t.Errorf("server.read_timeout = %v, want 10s", cfg.Server.ReadTimeout)
	}
	if cfg.Server.WriteTimeout != 10*time.Second {
		t.Errorf("server.write_timeout = %v, want 10s", cfg.Server.WriteTimeout)
	}

	if cfg.Auth.Enabled {
		t.Error("auth.enabled = true, want false")
	}
	if cfg.Auth.Token != "" {
		t.Errorf("auth.token = %q, want %q", cfg.Auth.Token, "")
	}

	if !cfg.Mdns.Enabled {
		t.Error("mdns.enabled = false, want true")
	}
	if cfg.Mdns.ServiceType != "_discord-proxy._tcp" {
		t.Errorf("mdns.service_type = %q, want %q", cfg.Mdns.ServiceType, "_discord-proxy._tcp")
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("logging.level = %q, want %q", cfg.Logging.Level, "info")
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("logging.format = %q, want %q", cfg.Logging.Format, "json")
	}
}

func TestConfigFileLoading(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, `
discord:
  client_id: "123456789"
  auto_reconnect: false
server:
  port: 9999
auth:
  enabled: true
  token: "test-secret"
`)

	v := newViperWithDefaults(t)
	v.AddConfigPath(dir)

	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Discord.ClientID != "123456789" {
		t.Errorf("discord.client_id = %q, want %q", cfg.Discord.ClientID, "123456789")
	}
	if cfg.Discord.AutoReconnect {
		t.Error("discord.auto_reconnect = true, want false")
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("server.port = %d, want 9999", cfg.Server.Port)
	}
	if !cfg.Auth.Enabled {
		t.Error("auth.enabled = false, want true")
	}
	if cfg.Auth.Token != "test-secret" {
		t.Errorf("auth.token = %q, want %q", cfg.Auth.Token, "test-secret")
	}
}

func TestEnvVarOverrides(t *testing.T) {
	envVars := map[string]string{
		"PROXY_DISCORD_CLIENT_ID":   "env-client-id",
		"PROXY_SERVER_PORT":         "7777",
		"PROXY_AUTH_TOKEN":          "env-token",
		"PROXY_AUTH_ENABLED":        "true",
		"PROXY_LOGGING_LEVEL":       "debug",
		"PROXY_MDNS_ENABLED":        "false",
		"PROXY_SERVER_READ_TIMEOUT": "30s",
	}

	for k, v := range envVars {
		t.Setenv(k, v)
	}

	v := newViperWithDefaults(t)
	setupEnv(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Discord.ClientID != "env-client-id" {
		t.Errorf("discord.client_id = %q, want %q", cfg.Discord.ClientID, "env-client-id")
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("server.port = %d, want 7777", cfg.Server.Port)
	}
	if cfg.Auth.Token != "env-token" {
		t.Errorf("auth.token = %q, want %q", cfg.Auth.Token, "env-token")
	}
	if !cfg.Auth.Enabled {
		t.Error("auth.enabled = false, want true")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("logging.level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Mdns.Enabled {
		t.Error("mdns.enabled = true, want false")
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("server.read_timeout = %v, want 30s", cfg.Server.ReadTimeout)
	}
}

func TestSetupLoggerJSON(t *testing.T) {
	logger, err := SetupLogger("info", "json")
	if err != nil {
		t.Fatalf("SetupLogger(json): %v", err)
	}
	defer logger.Sync()

	logger.Info("test log message", zap.String("key", "value"))
}

func TestSetupLoggerConsole(t *testing.T) {
	logger, err := SetupLogger("debug", "console")
	if err != nil {
		t.Fatalf("SetupLogger(console): %v", err)
	}
	defer logger.Sync()

	logger.Debug("test debug message")
	logger.Info("test info message")
}

func TestSetupLoggerInvalidLevel(t *testing.T) {
	_, err := SetupLogger("bogus", "json")
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func newViperWithDefaults(t *testing.T) *viper.Viper {
	t.Helper()
	v := viper.New()
	setDefaults(v)
	return v
}

func writeConfigFile(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
