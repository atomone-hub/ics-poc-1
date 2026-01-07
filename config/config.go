package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config represents configuration for consumer chains
type Config struct {
	Chains []ChainInfo
}

// ChainInfo represents configuration for a single consumer chain
type ChainInfo struct {
	ChainID     string
	Home        string
	GRPCAddress string
}

func (c *Config) Validate() error {
	if len(c.Chains) == 0 {
		return fmt.Errorf("at least one chain required")
	}

	for i, app := range c.Chains {
		if app.ChainID == "" {
			return fmt.Errorf("chain_id required at index %d", i)
		}
		if app.GRPCAddress == "" {
			return fmt.Errorf("address required for %s", app.ChainID)
		}
	}

	return nil
}

func DefaultConfig() *Config {
	return &Config{
		Chains: []ChainInfo{},
	}
}

func LoadConfig(configFile string) (*Config, error) {
	config := DefaultConfig()
	if configFile == "" {
		return config, nil
	}

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// Create the directory if it doesn't exist
		configDir := filepath.Dir(configFile)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}

		// Write default config
		viper.SetConfigFile(configFile)
		viper.Set("chains", config.Chains)
		if err := viper.WriteConfig(); err != nil {
			return nil, fmt.Errorf("failed to create config file: %w", err)
		}

		return config, nil
	}

	viper.SetConfigFile(configFile)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	if err := viper.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return config, nil
}
