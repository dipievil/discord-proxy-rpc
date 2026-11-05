package config

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/testcert"
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
	if cfg.Server.MaxWSConnections != 100 {
		t.Errorf("server.max_ws_connections = %d, want 100", cfg.Server.MaxWSConnections)
	}
	if cfg.Server.RateLimitPerIP != DefaultRateLimitPerIP {
		t.Errorf("server.rate_limit_per_ip = %d, want %d", cfg.Server.RateLimitPerIP, DefaultRateLimitPerIP)
	}
	if cfg.Server.RateLimitWindow != time.Minute {
		t.Errorf("server.rate_limit_window = %v, want 1m", cfg.Server.RateLimitWindow)
	}
	if len(cfg.Server.TrustedProxies) != 0 {
		t.Errorf("server.trusted_proxies = %v, want empty (RemoteAddr only)", cfg.Server.TrustedProxies)
	if cfg.Server.TLS.Enabled {
		t.Error("server.tls.enabled = true, want false")
	}
	if cfg.Server.TLS.CertFile != "" || cfg.Server.TLS.KeyFile != "" || cfg.Server.TLS.ClientCAFile != "" {
		t.Errorf("server.tls files = %q/%q/%q, want empty",
			cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile, cfg.Server.TLS.ClientCAFile)
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
  coalesce_interval: 10s
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
	if cfg.Discord.CoalesceInterval != 10*time.Second {
		t.Errorf("discord.coalesce_interval = %v, want 10s", cfg.Discord.CoalesceInterval)
	}
}

func TestDefaultRateLimitCoversDashboardReload(t *testing.T) {
	// One dashboard load costs index.html, style.css, app.js and the WebSocket
	// upgrade. The shipped budget must leave room for reloads and reconnect
	// attempts instead of letting the primary UI trip its own limiter.
	const perLoad = 4
	if reloads := DefaultRateLimitPerIP / perLoad; reloads < 3 {
		t.Errorf("DefaultRateLimitPerIP = %d: only %d dashboard loads fit in one window, want at least 3",
			DefaultRateLimitPerIP, reloads)
	}
}

func TestEnvVarOverrides(t *testing.T) {
	envVars := map[string]string{
		"PROXY_DISCORD_CLIENT_ID":         "env-client-id",
		"PROXY_SERVER_PORT":               "7777",
		"PROXY_AUTH_TOKEN":                "env-token",
		"PROXY_AUTH_ENABLED":              "true",
		"PROXY_LOGGING_LEVEL":             "debug",
		"PROXY_MDNS_ENABLED":              "false",
		"PROXY_SERVER_READ_TIMEOUT":       "30s",
		"PROXY_DISCORD_COALESCE_INTERVAL": "10s",
		"PROXY_SERVER_MAX_WS_CONNECTIONS": "50",
		"PROXY_SERVER_RATE_LIMIT_PER_IP":  "20",
		"PROXY_SERVER_RATE_LIMIT_WINDOW":  "30s",
		"PROXY_SERVER_TRUSTED_PROXIES":    "10.0.0.0/8,192.168.0.0/24",
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
	if cfg.Discord.CoalesceInterval != 10*time.Second {
		t.Errorf("discord.coalesce_interval = %v, want 10s", cfg.Discord.CoalesceInterval)
	}
	if cfg.Server.MaxWSConnections != 50 {
		t.Errorf("server.max_ws_connections = %d, want 50", cfg.Server.MaxWSConnections)
	}
	if cfg.Server.RateLimitPerIP != 20 {
		t.Errorf("server.rate_limit_per_ip = %d, want 20", cfg.Server.RateLimitPerIP)
	}
	if cfg.Server.RateLimitWindow != 30*time.Second {
		t.Errorf("server.rate_limit_window = %v, want 30s", cfg.Server.RateLimitWindow)
	}
	wantProxies := []string{"10.0.0.0/8", "192.168.0.0/24"}
	if len(cfg.Server.TrustedProxies) != len(wantProxies) {
		t.Fatalf("server.trusted_proxies = %v, want %v", cfg.Server.TrustedProxies, wantProxies)
	}
	for i, want := range wantProxies {
		if cfg.Server.TrustedProxies[i] != want {
			t.Errorf("server.trusted_proxies[%d] = %q, want %q", i, cfg.Server.TrustedProxies[i], want)
		}
	}
}

func TestLegacyProxyTokenIgnored(t *testing.T) {
	t.Setenv("PROXY_TOKEN", "legacy-token")

	v := newViperWithDefaults(t)
	setupEnv(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Auth.Token != "" {
		t.Errorf("auth.token = %q, want %q (legacy PROXY_TOKEN must be ignored)", cfg.Auth.Token, "")
	}
}

func TestProxyAuthTokenIsSoleEnvSource(t *testing.T) {
	t.Setenv("PROXY_TOKEN", "legacy-token")
	t.Setenv("PROXY_AUTH_TOKEN", "auth-token")

	v := newViperWithDefaults(t)
	setupEnv(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Auth.Token != "auth-token" {
		t.Errorf("auth.token = %q, want %q (PROXY_AUTH_TOKEN must win over legacy PROXY_TOKEN)", cfg.Auth.Token, "auth-token")
	}
}

func TestProxyAuthTokenOverridesConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, `
auth:
  enabled: true
  token: "file-secret"
`)
	t.Setenv("PROXY_AUTH_TOKEN", "env-secret")

	v := newViperWithDefaults(t)
	setupEnv(v)
	v.AddConfigPath(dir)

	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Auth.Token != "env-secret" {
		t.Errorf("auth.token = %q, want %q (PROXY_AUTH_TOKEN must win over config file)", cfg.Auth.Token, "env-secret")
	}
	if !cfg.Auth.Enabled {
		t.Error("auth.enabled = false, want true (config file value must be preserved)")
	}
}

func TestValidateMissingClientID(t *testing.T) {
	cfg := &Config{}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing discord.client_id")
	}
	if !strings.Contains(err.Error(), "discord.client_id") {
		t.Errorf("error %q does not name the field discord.client_id", err)
	}
}

func TestValidateBlankClientID(t *testing.T) {
	cfg := &Config{}
	cfg.Discord.ClientID = "   \t "

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for whitespace-only discord.client_id")
	}
	if !strings.Contains(err.Error(), "discord.client_id") {
		t.Errorf("error %q does not name the field discord.client_id", err)
	}
}

func TestValidateValidConfig(t *testing.T) {
	cfg := &Config{}
	cfg.Discord.ClientID = "123456789012345678"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with valid client_id: %v", err)
	}
	if cfg.Discord.ClientID != "123456789012345678" {
		t.Errorf("client_id not trimmed/preserved correctly: %q", cfg.Discord.ClientID)
	}
}

func TestValidateTrimsClientID(t *testing.T) {
	cfg := &Config{}
	cfg.Discord.ClientID = "  123456789012345678  "

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with padded client_id: %v", err)
	}
	if cfg.Discord.ClientID != "123456789012345678" {
		t.Errorf("client_id not trimmed: got %q", cfg.Discord.ClientID)
	}
}

func TestValidateNonNumericClientID(t *testing.T) {
	cfg := &Config{}
	cfg.Discord.ClientID = "1234567890abc"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for non-numeric discord.client_id")
	}
	if !strings.Contains(err.Error(), "not a valid Discord Application ID") {
		t.Errorf("error %q does not mention numeric validation", err)
	}
}

func TestValidateTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		proxies []string
		wantErr bool
	}{
		{"empty", nil, false},
		{"valid CIDRs", []string{"10.0.0.0/8", " 192.168.0.0/24 "}, false},
		{"valid IPv6 CIDR", []string{"fd00::/8"}, false},
		{"plain IP", []string{"10.0.0.1"}, true},
		{"garbage", []string{"not-a-cidr"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Discord.ClientID = "123456789012345678"
			cfg.Server.TrustedProxies = tt.proxies

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate(): want error, got nil")
				}
				if !strings.Contains(err.Error(), "server.trusted_proxies") {
					t.Errorf("error %q does not name server.trusted_proxies", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate(): %v", err)
			}
			for i, entry := range cfg.Server.TrustedProxies {
				if strings.TrimSpace(entry) != entry {
					t.Errorf("trusted_proxies[%d] = %q, want trimmed", i, entry)
				}
			}
		})
	}
}

func TestConfigFileTLS(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, `
server:
  tls:
    enabled: true
    cert_file: "/etc/proxy/tls.crt"
    key_file: "/etc/proxy/tls.key"
    client_ca_file: "/etc/proxy/clients-ca.crt"
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

	if !cfg.Server.TLS.Enabled {
		t.Error("server.tls.enabled = false, want true")
	}
	if cfg.Server.TLS.CertFile != "/etc/proxy/tls.crt" {
		t.Errorf("server.tls.cert_file = %q", cfg.Server.TLS.CertFile)
	}
	if cfg.Server.TLS.KeyFile != "/etc/proxy/tls.key" {
		t.Errorf("server.tls.key_file = %q", cfg.Server.TLS.KeyFile)
	}
	if cfg.Server.TLS.ClientCAFile != "/etc/proxy/clients-ca.crt" {
		t.Errorf("server.tls.client_ca_file = %q", cfg.Server.TLS.ClientCAFile)
	}
}

func TestTLSEnvVarOverrides(t *testing.T) {
	t.Setenv("PROXY_TLS_ENABLED", "true")
	t.Setenv("PROXY_TLS_CERT_FILE", "/etc/proxy/tls.crt")
	t.Setenv("PROXY_TLS_KEY_FILE", "/etc/proxy/tls.key")
	t.Setenv("PROXY_TLS_CLIENT_CA_FILE", "/etc/proxy/clients-ca.crt")

	v := newViperWithDefaults(t)
	setupEnv(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !cfg.Server.TLS.Enabled {
		t.Error("server.tls.enabled = false, want true")
	}
	if cfg.Server.TLS.CertFile != "/etc/proxy/tls.crt" {
		t.Errorf("server.tls.cert_file = %q", cfg.Server.TLS.CertFile)
	}
	if cfg.Server.TLS.KeyFile != "/etc/proxy/tls.key" {
		t.Errorf("server.tls.key_file = %q", cfg.Server.TLS.KeyFile)
	}
	if cfg.Server.TLS.ClientCAFile != "/etc/proxy/clients-ca.crt" {
		t.Errorf("server.tls.client_ca_file = %q", cfg.Server.TLS.ClientCAFile)
	}
}

func TestTLSMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  TLSConfig
		want string
	}{
		{"disabled", TLSConfig{}, TLSModeOff},
		{"server only", TLSConfig{Enabled: true}, TLSModeServer},
		{"mtls", TLSConfig{Enabled: true, ClientCAFile: "/ca.pem"}, TLSModeMTLS},
		{"client ca without enabled", TLSConfig{ClientCAFile: "/ca.pem"}, TLSModeOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Mode(); got != tc.want {
				t.Errorf("Mode() = %q, want %q", got, tc.want)
			}
			if got, want := tc.cfg.MTLSEnabled(), tc.want == TLSModeMTLS; got != want {
				t.Errorf("MTLSEnabled() = %v, want %v", got, want)
			}
		})
	}
}

func TestValidateTLS(t *testing.T) {
	material := newTLSMaterial(t)
	missingDir := t.TempDir()

	validServerTLS := TLSConfig{
		Enabled:  true,
		CertFile: material.certFile,
		KeyFile:  material.keyFile,
	}
	validMTLS := validServerTLS
	validMTLS.ClientCAFile = material.caFile

	cases := []struct {
		name    string
		tls     TLSConfig
		auth    AuthConfig
		wantErr string
	}{
		{name: "disabled", wantErr: ""},
		{name: "server only", tls: validServerTLS, wantErr: ""},
		{name: "server only with bearer auth", tls: validServerTLS, auth: AuthConfig{Enabled: true, Token: "secret"}, wantErr: ""},
		{name: "mtls", tls: validMTLS, wantErr: ""},
		{
			name:    "mtls with bearer auth is ambiguous",
			tls:     validMTLS,
			auth:    AuthConfig{Enabled: true, Token: "secret"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "enabled without cert",
			tls:     TLSConfig{Enabled: true, KeyFile: material.keyFile},
			wantErr: "server.tls.cert_file",
		},
		{
			name:    "enabled without key",
			tls:     TLSConfig{Enabled: true, CertFile: material.certFile},
			wantErr: "server.tls.key_file",
		},
		{
			name:    "missing cert file",
			tls:     TLSConfig{Enabled: true, CertFile: filepath.Join(missingDir, "absent.crt"), KeyFile: material.keyFile},
			wantErr: "absent.crt",
		},
		{
			name:    "missing key file",
			tls:     TLSConfig{Enabled: true, CertFile: material.certFile, KeyFile: filepath.Join(missingDir, "absent.key")},
			wantErr: "absent.key",
		},
		{
			name: "invalid cert pem",
			tls: TLSConfig{
				Enabled:  true,
				CertFile: writeFile(t, missingDir, "garbage.crt", "not a certificate"),
				KeyFile:  material.keyFile,
			},
			wantErr: "garbage.crt",
		},
		{
			name: "mismatched key",
			tls: TLSConfig{
				Enabled:  true,
				CertFile: material.certFile,
				KeyFile:  material.otherKeyFile,
			},
			wantErr: "server.tls: loading certificate pair",
		},
		{
			name: "missing client ca file",
			tls: TLSConfig{
				Enabled:      true,
				CertFile:     material.certFile,
				KeyFile:      material.keyFile,
				ClientCAFile: filepath.Join(missingDir, "absent-ca.crt"),
			},
			wantErr: "absent-ca.crt",
		},
		{
			name: "invalid client ca pem",
			tls: TLSConfig{
				Enabled:      true,
				CertFile:     material.certFile,
				KeyFile:      material.keyFile,
				ClientCAFile: writeFile(t, missingDir, "garbage-ca.crt", "not a CA"),
			},
			wantErr: "no valid PEM certificates",
		},
		{
			name:    "certificate files while disabled",
			tls:     TLSConfig{CertFile: material.certFile, KeyFile: material.keyFile},
			wantErr: "server.tls.enabled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Server: ServerConfig{TLS: tc.tls}, Auth: tc.auth}
			cfg.Discord.ClientID = "123456789012345678"

			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadConfigAcceptsMTLS(t *testing.T) {
	material := newTLSMaterial(t)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("PROXY_DISCORD_CLIENT_ID", "123456789012345678")
	t.Setenv("PROXY_TLS_ENABLED", "true")
	t.Setenv("PROXY_TLS_CERT_FILE", material.certFile)
	t.Setenv("PROXY_TLS_KEY_FILE", material.keyFile)
	t.Setenv("PROXY_TLS_CLIENT_CA_FILE", material.caFile)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with mTLS env: %v", err)
	}
	if !cfg.Server.TLS.MTLSEnabled() {
		t.Errorf("server.tls = %+v, want mTLS enabled", cfg.Server.TLS)
	}
}

func TestLoadConfigRejectsMTLSWithBearerAuth(t *testing.T) {
	material := newTLSMaterial(t)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("PROXY_DISCORD_CLIENT_ID", "123456789012345678")
	t.Setenv("PROXY_TLS_ENABLED", "true")
	t.Setenv("PROXY_TLS_CERT_FILE", material.certFile)
	t.Setenv("PROXY_TLS_KEY_FILE", material.keyFile)
	t.Setenv("PROXY_TLS_CLIENT_CA_FILE", material.caFile)
	t.Setenv("PROXY_AUTH_ENABLED", "true")
	t.Setenv("PROXY_AUTH_TOKEN", "secret")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() with mTLS + bearer auth: want error, got nil")
	} else if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q does not explain the conflict", err)
	}
}

func TestLoadConfigRejectsMissingClientID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PROXY_DISCORD_CLIENT_ID", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() with empty discord.client_id: want error, got nil")
	} else if !strings.Contains(err.Error(), "discord.client_id") {
		t.Errorf("error %q does not name discord.client_id", err)
	}
}

func TestLoadConfigAcceptsClientIDFromEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PROXY_DISCORD_CLIENT_ID", "123456789012345678")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with PROXY_DISCORD_CLIENT_ID set: %v", err)
	}
	if cfg.Discord.ClientID != "123456789012345678" {
		t.Errorf("discord.client_id = %q, want %q", cfg.Discord.ClientID, "123456789012345678")
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

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

type tlsMaterial struct {
	caFile       string
	certFile     string
	keyFile      string
	otherKeyFile string
}

// newTLSMaterial writes a CA, a server certificate for 127.0.0.1 signed by it
// and a second, unrelated key, so the validation cases can exercise a real
// certificate/CA pair and a mismatched key.
func newTLSMaterial(t *testing.T) tlsMaterial {
	t.Helper()

	dir := t.TempDir()
	ca, err := testcert.NewAuthority("test-ca")
	if err != nil {
		t.Fatalf("new CA: %v", err)
	}
	certPEM, keyPEM, err := ca.Issue("localhost",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("issue server certificate: %v", err)
	}
	certFile, keyFile, err := testcert.WritePair(dir, "server", certPEM, keyPEM)
	if err != nil {
		t.Fatalf("write server pair: %v", err)
	}

	otherCA, err := testcert.NewAuthority("other-ca")
	if err != nil {
		t.Fatalf("new other CA: %v", err)
	}
	otherCertPEM, otherKeyPEM, err := otherCA.Issue("other",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		[]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("issue other certificate: %v", err)
	}
	_, otherKeyFile, err := testcert.WritePair(dir, "other", otherCertPEM, otherKeyPEM)
	if err != nil {
		t.Fatalf("write other pair: %v", err)
	}

	return tlsMaterial{
		caFile:       writeFile(t, dir, "ca.crt", string(ca.CertPEM())),
		certFile:     certFile,
		keyFile:      keyFile,
		otherKeyFile: otherKeyFile,
	}
}
