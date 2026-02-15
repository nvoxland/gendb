package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file and returns the parsed Config.
// If the file does not exist, it returns the default config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	return cfg, nil
}

// Config holds the GenDB configuration.
type Config struct {
	LogLevel   string           `yaml:"log_level"`
	LLM        LLMConfig        `yaml:"llm"`
	Generation GenerationConfig `yaml:"generation"`
}

type LLMConfig struct {
	Provider string `yaml:"provider"` // ollama | openai | custom
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
}

type GenerationConfig struct {
	DefaultRows int                    `yaml:"default_rows"`
	Seed        int64                  `yaml:"seed"`
	Tables      map[string]TableConfig `yaml:"tables"`
	ColumnRules []ColumnRule           `yaml:"column_rules"`
}

type TableConfig struct {
	Rows    int                     `yaml:"rows"`
	Columns map[string]ColumnConfig `yaml:"columns"`
}

type ColumnConfig struct {
	Generator string   `yaml:"generator"`
	Prompt    string   `yaml:"prompt,omitempty"`
	Values    []string `yaml:"values,omitempty"`
	Format    string   `yaml:"format,omitempty"`
	Template  string   `yaml:"template,omitempty"`
}

type ColumnRule struct {
	Pattern   string `yaml:"pattern"`
	Generator string `yaml:"generator"`
	Format    string `yaml:"format,omitempty"`
	Template  string `yaml:"template,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		LogLevel: "info",
		LLM: LLMConfig{
			Provider: "ollama",
			Model:    "llama3.2",
			BaseURL:  "http://localhost:11434/v1",
		},
		Generation: GenerationConfig{
			DefaultRows: 100,
			Seed:        42,
		},
	}
}
