package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"

	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// Config represents configuration for consumer chains
type Config struct {
	Chains []ChainInfo `mapstructure:"chains"`
}

// ChainInfo represents configuration for a single consumer chain
type ChainInfo struct {
	ChainID     string `mapstructure:"chain_id"`
	GRPCAddress string `mapstructure:"grpc_address"`
	Home        string `mapstructure:"home"`
}

// GenesisFile returns the path to the genesis file for the chain
func (c *ChainInfo) GenesisFile() string {
	return filepath.Join(c.Home, "config", "genesis.json")
}

// LoadGenesisAppState loads and returns the app state bytes from the genesis file
func (c *ChainInfo) LoadGenesisAppState() ([]byte, error) {
	genesisFile := c.GenesisFile()
	appGenesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load genesis from %s: %w", genesisFile, err)
	}
	return appGenesis.AppState, nil
}

func (c *Config) Validate() error {
	for i, app := range c.Chains {
		if app.ChainID == "" {
			return fmt.Errorf("chain_id required at index %d", i)
		}
		if app.GRPCAddress == "" {
			return fmt.Errorf("address required for %s", app.ChainID)
		}
		if app.Home == "" {
			return fmt.Errorf("home required for %s", app.ChainID)
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
