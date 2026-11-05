package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type DiscordConfig struct {
	ClientID              string        `mapstructure:"client_id"       yaml:"client_id"`
	AutoReconnect         bool          `mapstructure:"auto_reconnect"  yaml:"auto_reconnect"`
	ReconnectBaseInterval time.Duration `mapstructure:"reconnect_base_interval" yaml:"reconnect_base_interval"`
	MaxReconnectInterval  time.Duration `mapstructure:"max_reconnect_interval"  yaml:"max_reconnect_interval"`
	HealthCheckInterval   time.Duration `mapstructure:"health_check_interval"   yaml:"health_check_interval"`
	HealthCheckTimeout    time.Duration `mapstructure:"health_check_timeout"    yaml:"health_check_timeout"`
	CoalesceInterval      time.Duration `mapstructure:"coalesce_interval"       yaml:"coalesce_interval"`
}

// TLS transport modes reported by TLSConfig.Mode and advertised over mDNS.
const (
	TLSModeOff    = "off"
	TLSModeServer = "server"
	TLSModeMTLS   = "mtls"
)

// TLSConfig controls the HTTPS listener. Enabled alone serves server-side TLS;
// setting ClientCAFile additionally requires and verifies a client certificate
// (mTLS), which replaces bearer-token auth.
type TLSConfig struct {
	Enabled      bool   `mapstructure:"enabled"        yaml:"enabled"`
	CertFile     string `mapstructure:"cert_file"      yaml:"cert_file"`
	KeyFile      string `mapstructure:"key_file"       yaml:"key_file"`
	ClientCAFile string `mapstructure:"client_ca_file" yaml:"client_ca_file"`
}

// MTLSEnabled reports whether clients must present a certificate signed by
// ClientCAFile.
func (t TLSConfig) MTLSEnabled() bool {
	return t.Enabled && t.ClientCAFile != ""
}

// Mode names the transport authentication mode: TLSModeOff, TLSModeServer or
// TLSModeMTLS.
func (t TLSConfig) Mode() string {
	switch {
	case !t.Enabled:
		return TLSModeOff
	case t.ClientCAFile != "":
		return TLSModeMTLS
	default:
		return TLSModeServer
	}
}

type ServerConfig struct {
	Host             string        `mapstructure:"host"              yaml:"host"`
	Port             int           `mapstructure:"port"              yaml:"port"`
	WsPath           string        `mapstructure:"ws_path"           yaml:"ws_path"`
	ReadTimeout      time.Duration `mapstructure:"read_timeout"      yaml:"read_timeout"`
	WriteTimeout     time.Duration `mapstructure:"write_timeout"     yaml:"write_timeout"`
	MaxWSConnections int           `mapstructure:"max_ws_connections" yaml:"max_ws_connections"`
	RateLimitPerIP   int           `mapstructure:"rate_limit_per_ip"  yaml:"rate_limit_per_ip"`
	RateLimitWindow  time.Duration `mapstructure:"rate_limit_window"  yaml:"rate_limit_window"`
	TrustedProxies   []string      `mapstructure:"trusted_proxies"    yaml:"trusted_proxies"`
	TLS              TLSConfig     `mapstructure:"tls"               yaml:"tls"`
}

// DefaultRateLimitPerIP is the shipped per-IP request budget. One dashboard
// load costs four requests (index.html, style.css, app.js and the WebSocket
// upgrade) and a reconnecting tab adds a few more, so the default leaves room
// for reloads instead of letting the primary UI trip its own limiter.
const DefaultRateLimitPerIP = 60

type AuthConfig struct {
	Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
	Token   string `mapstructure:"token"   yaml:"token"`
}

type MdnsConfig struct {
	Enabled      bool   `mapstructure:"enabled"       yaml:"enabled"`
	InstanceName string `mapstructure:"instance_name" yaml:"instance_name"`
	ServiceType  string `mapstructure:"service_type"  yaml:"service_type"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"  yaml:"level"`
	Format string `mapstructure:"format" yaml:"format"`
}

type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

type Config struct {
	Discord DiscordConfig `mapstructure:"discord" yaml:"discord"`
	Server  ServerConfig  `mapstructure:"server"  yaml:"server"`
	Auth    AuthConfig    `mapstructure:"auth"    yaml:"auth"`
	Mdns    MdnsConfig    `mapstructure:"mdns"    yaml:"mdns"`
	Logging LoggingConfig `mapstructure:"logging" yaml:"logging"`
	Metrics MetricsConfig `mapstructure:"metrics" yaml:"metrics"`
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("discord.client_id", "")
	v.SetDefault("discord.auto_reconnect", true)
	v.SetDefault("discord.reconnect_base_interval", 5*time.Second)
	v.SetDefault("discord.max_reconnect_interval", 60*time.Second)
	v.SetDefault("discord.health_check_interval", 30*time.Second)
	v.SetDefault("discord.health_check_timeout", 10*time.Second)
	v.SetDefault("discord.coalesce_interval", 5*time.Second)

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8765)
	v.SetDefault("server.ws_path", "/ws")
	v.SetDefault("server.read_timeout", 10*time.Second)
	v.SetDefault("server.write_timeout", 10*time.Second)
	v.SetDefault("server.max_ws_connections", 100)
	v.SetDefault("server.rate_limit_per_ip", DefaultRateLimitPerIP)
	v.SetDefault("server.rate_limit_window", time.Minute)
	v.SetDefault("server.trusted_proxies", []string{})

	v.SetDefault("server.tls.enabled", false)
	v.SetDefault("server.tls.cert_file", "")
	v.SetDefault("server.tls.key_file", "")
	v.SetDefault("server.tls.client_ca_file", "")

	v.SetDefault("auth.enabled", false)
	v.SetDefault("auth.token", "")

	v.SetDefault("mdns.enabled", true)
	v.SetDefault("mdns.instance_name", "")
	v.SetDefault("mdns.service_type", "_discord-proxy._tcp")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	v.SetDefault("metrics.enabled", false)
}

func setupEnv(v *viper.Viper) {
	v.SetEnvPrefix("PROXY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	_ = v.BindEnv("discord.client_id", "PROXY_DISCORD_CLIENT_ID")
	_ = v.BindEnv("discord.auto_reconnect", "PROXY_DISCORD_AUTO_RECONNECT")
	_ = v.BindEnv("discord.reconnect_base_interval", "PROXY_DISCORD_RECONNECT_BASE_INTERVAL")
	_ = v.BindEnv("discord.max_reconnect_interval", "PROXY_DISCORD_MAX_RECONNECT_INTERVAL")
	_ = v.BindEnv("discord.health_check_interval", "PROXY_DISCORD_HEALTH_CHECK_INTERVAL")
	_ = v.BindEnv("discord.health_check_timeout", "PROXY_DISCORD_HEALTH_CHECK_TIMEOUT")
	_ = v.BindEnv("discord.coalesce_interval", "PROXY_DISCORD_COALESCE_INTERVAL")

	_ = v.BindEnv("server.host", "PROXY_SERVER_HOST")
	_ = v.BindEnv("server.port", "PROXY_SERVER_PORT")
	_ = v.BindEnv("server.ws_path", "PROXY_SERVER_WS_PATH")
	_ = v.BindEnv("server.read_timeout", "PROXY_SERVER_READ_TIMEOUT")
	_ = v.BindEnv("server.write_timeout", "PROXY_SERVER_WRITE_TIMEOUT")
	_ = v.BindEnv("server.max_ws_connections", "PROXY_SERVER_MAX_WS_CONNECTIONS")
	_ = v.BindEnv("server.rate_limit_per_ip", "PROXY_SERVER_RATE_LIMIT_PER_IP")
	_ = v.BindEnv("server.rate_limit_window", "PROXY_SERVER_RATE_LIMIT_WINDOW")
	_ = v.BindEnv("server.trusted_proxies", "PROXY_SERVER_TRUSTED_PROXIES")

	_ = v.BindEnv("server.tls.enabled", "PROXY_TLS_ENABLED")
	_ = v.BindEnv("server.tls.cert_file", "PROXY_TLS_CERT_FILE")
	_ = v.BindEnv("server.tls.key_file", "PROXY_TLS_KEY_FILE")
	_ = v.BindEnv("server.tls.client_ca_file", "PROXY_TLS_CLIENT_CA_FILE")

	_ = v.BindEnv("auth.enabled", "PROXY_AUTH_ENABLED")
	_ = v.BindEnv("auth.token", "PROXY_AUTH_TOKEN")

	_ = v.BindEnv("mdns.enabled", "PROXY_MDNS_ENABLED")
	_ = v.BindEnv("mdns.instance_name", "PROXY_MDNS_INSTANCE_NAME")
	_ = v.BindEnv("mdns.service_type", "PROXY_MDNS_SERVICE_TYPE")

	_ = v.BindEnv("logging.level", "PROXY_LOGGING_LEVEL")
	_ = v.BindEnv("logging.format", "PROXY_LOGGING_FORMAT")

	_ = v.BindEnv("metrics.enabled", "PROXY_METRICS_ENABLED")
}

func configFileSearchPaths() []string {
	paths := []string{"."}

	if dir, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(dir, ".config", "discord-proxy-rpc"))
	}

	paths = append(paths, "/etc/discord-proxy-rpc")

	return paths
}

func LoadConfig() (*Config, error) {
	v := viper.New()

	setDefaults(v)
	setupEnv(v)

	v.SetConfigName("config")
	v.SetConfigType("yaml")

	for _, p := range configFileSearchPaths() {
		v.AddConfigPath(p)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	id := strings.TrimSpace(c.Discord.ClientID)
	if id == "" {
		return errors.New("discord.client_id is required but empty: set it in config.yaml under \"discord.client_id\" or via the PROXY_DISCORD_CLIENT_ID environment variable (Discord Developer Portal > General Information > Application ID)")
	}
	if strings.ContainsFunc(id, func(r rune) bool { return r < '0' || r > '9' }) {
		return fmt.Errorf("discord.client_id %q is not a valid Discord Application ID: expected digits only (Discord Developer Portal > General Information > Application ID)", id)
	}
	c.Discord.ClientID = id

	for i, entry := range c.Server.TrustedProxies {
		network := strings.TrimSpace(entry)
		if _, _, err := net.ParseCIDR(network); err != nil {
			return fmt.Errorf("server.trusted_proxies entry %q is not a CIDR block: expected e.g. 10.0.0.0/8 or 192.168.0.0/24", entry)
		}
		c.Server.TrustedProxies[i] = network
	}

	if err := c.validateTLS(); err != nil {
		return err
	}

	return nil
}

// validateTLS rejects an inconsistent TLS configuration at load time. Certificate
// material is parsed here so a bad path or an invalid PEM fails at startup with
// the offending file named, never as a silent fallback to plaintext.
func (c *Config) validateTLS() error {
	tlsCfg := c.Server.TLS

	if !tlsCfg.Enabled {
		if tlsCfg.CertFile != "" || tlsCfg.KeyFile != "" || tlsCfg.ClientCAFile != "" {
			return errors.New("server.tls.cert_file, server.tls.key_file or server.tls.client_ca_file is set while server.tls.enabled is false: set server.tls.enabled: true (PROXY_TLS_ENABLED) or remove the certificate settings")
		}
		return nil
	}

	if tlsCfg.CertFile == "" {
		return errors.New("server.tls.cert_file is required when server.tls.enabled is true: set the PEM certificate path in config.yaml or via PROXY_TLS_CERT_FILE")
	}
	if tlsCfg.KeyFile == "" {
		return errors.New("server.tls.key_file is required when server.tls.enabled is true: set the PEM private key path in config.yaml or via PROXY_TLS_KEY_FILE")
	}
	if tlsCfg.ClientCAFile != "" && c.Auth.Enabled {
		return errors.New("server.tls.client_ca_file and auth.enabled are mutually exclusive: mTLS authenticates clients with certificates, so disable auth.enabled (PROXY_AUTH_ENABLED=false) or remove server.tls.client_ca_file")
	}
	if _, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile); err != nil {
		return fmt.Errorf("server.tls: loading certificate pair (cert_file %q, key_file %q): %w", tlsCfg.CertFile, tlsCfg.KeyFile, err)
	}

	if tlsCfg.ClientCAFile == "" {
		return nil
	}

	caPEM, err := os.ReadFile(tlsCfg.ClientCAFile)
	if err != nil {
		return fmt.Errorf("server.tls.client_ca_file %q: %w", tlsCfg.ClientCAFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("server.tls.client_ca_file %q contains no valid PEM certificates", tlsCfg.ClientCAFile)
	}

	return nil
}

func SetupLogger(level, format string) (*zap.Logger, error) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}

	var cfg zap.Config
	if format == "console" {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	cfg.Level.SetLevel(lvl)

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("building logger: %w", err)
	}

	return logger, nil
}
