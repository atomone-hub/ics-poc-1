package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_WithValidFile(t *testing.T) {
	configPath := filepath.Join("testdata", "config.toml")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config to be non-nil")
	}

	if len(cfg.Chains) != 2 {
		t.Errorf("expected 2 chains, got %d", len(cfg.Chains))
	}

	// Verify first chain
	if cfg.Chains[0].ChainID != "chain-1" {
		t.Errorf("expected chain_id 'chain-1', got '%s'", cfg.Chains[0].ChainID)
	}
	if cfg.Chains[0].GRPCAddress != "tcp://localhost:9090" {
		t.Errorf("expected address 'tcp://localhost:9090', got '%s'", cfg.Chains[0].GRPCAddress)
	}
	if cfg.Chains[0].Home != "/tmp/chain1" {
		t.Errorf("expected home '/tmp/chain1', got '%s'", cfg.Chains[0].Home)
	}

	// Verify second chain
	if cfg.Chains[1].ChainID != "chain-2" {
		t.Errorf("expected chain_id 'chain-2', got '%s'", cfg.Chains[1].ChainID)
	}
	if cfg.Chains[1].GRPCAddress != "tcp://localhost:9091" {
		t.Errorf("expected address 'tcp://localhost:9091', got '%s'", cfg.Chains[1].GRPCAddress)
	}
	if cfg.Chains[1].Home != "/tmp/chain2" {
		t.Errorf("expected home '/tmp/chain2', got '%s'", cfg.Chains[1].Home)
	}
}

func TestLoadConfig_WithEmptyPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("expected no error with empty path, got: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected default config to be non-nil")
	}

	// Should return default config
	defaultCfg := DefaultConfig()
	if len(cfg.Chains) != len(defaultCfg.Chains) {
		t.Errorf("expected %d chains in default config, got %d", len(defaultCfg.Chains), len(cfg.Chains))
	}
}

func TestLoadConfig_WithInvalidToml(t *testing.T) {
	// Create a temporary invalid TOML file
	tmpFile, err := os.CreateTemp("", "invalid-*.toml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write invalid TOML content
	if _, err := tmpFile.WriteString("[[chains]\ninvalid toml syntax"); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	if err == nil {
		t.Fatal("expected error for invalid TOML, got nil")
	}
}

func TestValidate_Success(t *testing.T) {
	cfg := &Config{
		Chains: []ChainInfo{
			{
				ChainID:     "test-chain",
				GRPCAddress: "grpc://localhost:9090",
				Home:        "/home/test/.test-chain",
			},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected no validation error, got: %v", err)
	}
}

func TestValidate_MissingChainID(t *testing.T) {
	cfg := &Config{
		Chains: []ChainInfo{
			{
				ChainID:     "",
				GRPCAddress: "grpc://localhost:9090",
				Home:        "/home/test/.test-chain",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing chain_id, got nil")
	}
}

func TestValidate_MissingAddress(t *testing.T) {
	cfg := &Config{
		Chains: []ChainInfo{
			{
				ChainID:     "test-chain",
				GRPCAddress: "",
				Home:        "/home/test/.test-chain",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing address, got nil")
	}
}

func TestValidate_MissingHome(t *testing.T) {
	cfg := &Config{
		Chains: []ChainInfo{
			{
				ChainID:     "test-chain",
				GRPCAddress: "grpc://localhost:9090",
				Home:        "",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing home, got nil")
	}
}

func TestValidate_MultipleChains(t *testing.T) {
	cfg := &Config{
		Chains: []ChainInfo{
			{
				ChainID:     "chain-1",
				GRPCAddress: "grpc://localhost:9090",
				Home:        "/home/test/.chain-1",
			},
			{
				ChainID:     "chain-2",
				GRPCAddress: "grpc://localhost:9091",
				Home:        "/home/test/.chain-2",
			},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected no validation error for multiple valid chains, got: %v", err)
	}
}
