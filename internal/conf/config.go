package conf

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	App    AppConfig    `mapstructure:"app"`
	Server ServerConfig `mapstructure:"server"`
	Redis  RedisConfig  `mapstructure:"redis"`
	Stream StreamConfig `mapstructure:"stream"`
	AI     AIConfig     `mapstructure:"ai"`
}

type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
}

type ServerConfig struct {
	Port     int `mapstructure:"port"`
	GrpcPort int `mapstructure:"grpc_port"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type StreamConfig struct {
	// WindowSizeSeconds is the tumbling window duration for the aggregator.
	WindowSizeSeconds int `mapstructure:"window_size_seconds"`
	// MaxBatchSize caps the number of events sampled per window before sending to LLM.
	MaxBatchSize int `mapstructure:"max_batch_size"`
}

type AIConfig struct {
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
}

type LoadOptions struct {
	ConfigFile string
	ConfigDir  string
	ConfigName string
	ConfigType string
	EnvPrefix  string
}

// Load keeps backward-compatibility for callers that pass a config directory.
func Load(path string) (*Config, error) {
	opts := DefaultLoadOptions()
	opts.ConfigDir = path
	return LoadWithOptions(opts)
}

func DefaultLoadOptions() LoadOptions {
	return LoadOptions{
		ConfigName: "config",
		ConfigType: "yaml",
		EnvPrefix:  "DDQ",
	}
}

// LoadWithOptions loads config with precedence:
// flags/options > environment variables > config file > defaults.
func LoadWithOptions(opts LoadOptions) (*Config, error) {
	opts = resolveOptions(opts)

	v := viper.New()
	applyDefaults(v)
	configureEnv(v, opts.EnvPrefix)
	configureConfigSource(v, opts)

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		log.Printf("Config file not found, using defaults and env vars")
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &c, nil
}

func resolveOptions(opts LoadOptions) LoadOptions {
	resolved := DefaultLoadOptions()

	if opts.ConfigName != "" {
		resolved.ConfigName = opts.ConfigName
	}
	if opts.ConfigType != "" {
		resolved.ConfigType = opts.ConfigType
	}
	if opts.EnvPrefix != "" {
		resolved.EnvPrefix = opts.EnvPrefix
	}

	if opts.ConfigFile != "" {
		resolved.ConfigFile = opts.ConfigFile
	} else if envFile := strings.TrimSpace(os.Getenv("DDQ_CONFIG_FILE")); envFile != "" {
		resolved.ConfigFile = envFile
	}

	if opts.ConfigDir != "" {
		resolved.ConfigDir = opts.ConfigDir
	} else if envDir := strings.TrimSpace(os.Getenv("DDQ_CONFIG_DIR")); envDir != "" {
		resolved.ConfigDir = envDir
	}

	return resolved
}

func configureEnv(v *viper.Viper, prefix string) {
	v.SetEnvPrefix(prefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
}

func configureConfigSource(v *viper.Viper, opts LoadOptions) {
	if opts.ConfigFile != "" {
		v.SetConfigFile(filepath.Clean(opts.ConfigFile))
		return
	}

	v.SetConfigName(opts.ConfigName)
	v.SetConfigType(opts.ConfigType)

	if opts.ConfigDir != "" {
		v.AddConfigPath(filepath.Clean(opts.ConfigDir))
		return
	}

	v.AddConfigPath(".")
	v.AddConfigPath("config")
}

func applyDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "async-task-platform")
	v.SetDefault("app.env", "local")

	v.SetDefault("server.port", 8080)
	v.SetDefault("server.grpc_port", 9090)

	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	v.SetDefault("stream.window_size_seconds", 5)
	v.SetDefault("stream.max_batch_size", 200)

	v.SetDefault("ai.base_url", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	v.SetDefault("ai.model", "qwen-plus")
}
