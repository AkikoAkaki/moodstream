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
stream:
  window_size_seconds: 10
  max_batch_size: 100
ai:
  api_key: test-key
  model: qwen-turbo
`
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
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
	if cfg.Stream.WindowSizeSeconds != 10 {
		t.Fatalf("Stream.WindowSizeSeconds = %d, want 10", cfg.Stream.WindowSizeSeconds)
	}
	if cfg.Stream.MaxBatchSize != 100 {
		t.Fatalf("Stream.MaxBatchSize = %d, want 100", cfg.Stream.MaxBatchSize)
	}
	if cfg.AI.Model != "qwen-turbo" {
		t.Fatalf("AI.Model = %q, want qwen-turbo", cfg.AI.Model)
	}
}

func TestLoadWithOptions_EnvOverride(t *testing.T) {
	t.Setenv("DDQ_REDIS_ADDR", "redis-prod:6380")
	t.Setenv("DDQ_REDIS_DB", "5")

	cfg, err := LoadWithOptions(LoadOptions{})
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

func TestLoadWithOptions_Defaults(t *testing.T) {
	t.Parallel()

	cfg, err := LoadWithOptions(LoadOptions{
		ConfigDir: filepath.Join(t.TempDir(), "missing"),
	})
	if err != nil {
		t.Fatalf("LoadWithOptions() unexpected error: %v", err)
	}

	if cfg.App.Name != "moodstream" {
		t.Fatalf("App.Name = %q, want moodstream", cfg.App.Name)
	}
	if cfg.Server.GrpcPort != 9090 {
		t.Fatalf("Server.GrpcPort = %d, want 9090", cfg.Server.GrpcPort)
	}
	if cfg.Stream.WindowSizeSeconds != 5 {
		t.Fatalf("Stream.WindowSizeSeconds = %d, want 5", cfg.Stream.WindowSizeSeconds)
	}
	if cfg.AI.Model != "qwen-plus" {
		t.Fatalf("AI.Model = %q, want qwen-plus", cfg.AI.Model)
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
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadWithOptions(LoadOptions{ConfigDir: tmpDir})
	if err == nil {
		t.Fatal("LoadWithOptions() expected error for invalid YAML")
	}
}
