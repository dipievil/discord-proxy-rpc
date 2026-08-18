package config

import (
	"fmt"
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
}

type ServerConfig struct {
	Host         string        `mapstructure:"host"          yaml:"host"`
	Port         int           `mapstructure:"port"          yaml:"port"`
	WsPath       string        `mapstructure:"ws_path"       yaml:"ws_path"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"  yaml:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`
}

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

type Config struct {
	Discord DiscordConfig `mapstructure:"discord" yaml:"discord"`
	Server  ServerConfig  `mapstructure:"server"  yaml:"server"`
	Auth    AuthConfig    `mapstructure:"auth"    yaml:"auth"`
	Mdns    MdnsConfig    `mapstructure:"mdns"    yaml:"mdns"`
	Logging LoggingConfig `mapstructure:"logging" yaml:"logging"`
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("discord.client_id", "")
	v.SetDefault("discord.auto_reconnect", true)
	v.SetDefault("discord.reconnect_base_interval", 5*time.Second)
	v.SetDefault("discord.max_reconnect_interval", 60*time.Second)

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8765)
	v.SetDefault("server.ws_path", "/ws")
	v.SetDefault("server.read_timeout", 10*time.Second)
	v.SetDefault("server.write_timeout", 10*time.Second)

	v.SetDefault("auth.enabled", false)
	v.SetDefault("auth.token", "")

	v.SetDefault("mdns.enabled", true)
	v.SetDefault("mdns.instance_name", "")
	v.SetDefault("mdns.service_type", "_discord-proxy._tcp")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

func setupEnv(v *viper.Viper) {
	v.SetEnvPrefix("PROXY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	_ = v.BindEnv("discord.client_id", "PROXY_DISCORD_CLIENT_ID")
	_ = v.BindEnv("discord.auto_reconnect", "PROXY_DISCORD_AUTO_RECONNECT")
	_ = v.BindEnv("discord.reconnect_base_interval", "PROXY_DISCORD_RECONNECT_BASE_INTERVAL")
	_ = v.BindEnv("discord.max_reconnect_interval", "PROXY_DISCORD_MAX_RECONNECT_INTERVAL")

	_ = v.BindEnv("server.host", "PROXY_SERVER_HOST")
	_ = v.BindEnv("server.port", "PROXY_SERVER_PORT")
	_ = v.BindEnv("server.ws_path", "PROXY_SERVER_WS_PATH")
	_ = v.BindEnv("server.read_timeout", "PROXY_SERVER_READ_TIMEOUT")
	_ = v.BindEnv("server.write_timeout", "PROXY_SERVER_WRITE_TIMEOUT")

	_ = v.BindEnv("auth.enabled", "PROXY_AUTH_ENABLED")
	_ = v.BindEnv("auth.token", "PROXY_AUTH_TOKEN")

	_ = v.BindEnv("mdns.enabled", "PROXY_MDNS_ENABLED")
	_ = v.BindEnv("mdns.instance_name", "PROXY_MDNS_INSTANCE_NAME")
	_ = v.BindEnv("mdns.service_type", "PROXY_MDNS_SERVICE_TYPE")

	_ = v.BindEnv("logging.level", "PROXY_LOGGING_LEVEL")
	_ = v.BindEnv("logging.format", "PROXY_LOGGING_FORMAT")
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

	return &cfg, nil
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
