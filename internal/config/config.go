package config

import (
	"encoding/json"
	"fmt"
	"gator/internal/database"
	"os"
	"path/filepath"
)

type Config struct {
	DBURL           string `json:"db_url"`
	CurrentUserName string `json:"current_user_name,omitempty"`
}

type State struct {
	Config *Config
	DB  *database.Queries

}

type Command struct {
	Name string
	Args []string
}

func Read() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("unable to get home directory: %v", err)
	}
	configFilePath := fmt.Sprintf("%s/.gatorconfig.json", homeDir)
	file, err := os.Open(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening config file: %v", err)
	}
	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("error decoding config file: %v", err)
	}
	return &config, nil
}

func (c *Config) SetUser(username string) error {
	c.CurrentUserName = username
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("unable to get home directory: %v", err)
	}
	configPath := filepath.Join(homeDir, ".gatorconfig.json")
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("unable to open config file for writing: %v", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("unable to write to config file: %v", err)
	}
	return nil
}
