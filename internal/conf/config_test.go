package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_FromDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configYAML := `
app:
  name: test-app
  env: test
server:
  port: 8081
  grpc_port: 9091
redis:
  addr: localhost:6380
  db: 1
queue:
  visibility_timeout: 120
  watchdog_interval: 20
  max_retries: 5
`

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.App.Name != "test-app" {
		t.Fatalf("App.Name = %q, want test-app", cfg.App.Name)
	}
	if cfg.Server.Port != 8081 {
		t.Fatalf("Server.Port = %d, want 8081", cfg.Server.Port)
	}
	if cfg.Server.GrpcPort != 9091 {
		t.Fatalf("Server.GrpcPort = %d, want 9091", cfg.Server.GrpcPort)
	}
	if cfg.Redis.Addr != "localhost:6380" {
		t.Fatalf("Redis.Addr = %q, want localhost:6380", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 1 {
		t.Fatalf("Redis.DB = %d, want 1", cfg.Redis.DB)
	}
	if cfg.Queue.VisibilityTimeout != 120 {
		t.Fatalf("Queue.VisibilityTimeout = %d, want 120", cfg.Queue.VisibilityTimeout)
	}
	if cfg.Queue.WatchdogInterval != 20 {
		t.Fatalf("Queue.WatchdogInterval = %d, want 20", cfg.Queue.WatchdogInterval)
	}
	if cfg.Queue.MaxRetries != 5 {
		t.Fatalf("Queue.MaxRetries = %d, want 5", cfg.Queue.MaxRetries)
	}
}

func TestLoadWithOptions_ConfigFileFlagAndEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configYAML := `
redis:
  addr: localhost:6379
  db: 0
`
	configPath := filepath.Join(tmpDir, "custom.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DDQ_REDIS_ADDR", "redis-prod:6380")
	t.Setenv("DDQ_REDIS_DB", "5")

	cfg, err := LoadWithOptions(LoadOptions{
		ConfigFile: configPath,
	})
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}

	if cfg.Redis.Addr != "redis-prod:6380" {
		t.Fatalf("Redis.Addr = %q, want redis-prod:6380", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 5 {
		t.Fatalf("Redis.DB = %d, want 5", cfg.Redis.DB)
	}
}

func TestLoadWithOptions_UsesDefaultsWhenFileMissing(t *testing.T) {
	t.Parallel()

	cfg, err := LoadWithOptions(LoadOptions{
		ConfigDir: filepath.Join(t.TempDir(), "missing"),
	})
	if err != nil {
		t.Fatalf("LoadWithOptions() unexpected error: %v", err)
	}

	if cfg.App.Name != "async-task-platform" {
		t.Fatalf("App.Name = %q, want async-task-platform", cfg.App.Name)
	}
	if cfg.Server.GrpcPort != 9090 {
		t.Fatalf("Server.GrpcPort = %d, want 9090", cfg.Server.GrpcPort)
	}
	if cfg.Queue.MaxRetries != 3 {
		t.Fatalf("Queue.MaxRetries = %d, want 3", cfg.Queue.MaxRetries)
	}
}

func TestLoadWithOptions_InvalidYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	invalidYAML := `
this is not valid YAML
  - invalid
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadWithOptions(LoadOptions{ConfigDir: tmpDir})
	if err == nil {
		t.Fatal("LoadWithOptions() expected error for invalid YAML")
	}
}
