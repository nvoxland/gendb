package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultConfigFile = "autodb.yaml"

type Config struct {
	Connection ConnectionConfig `yaml:"connection"`
	LLM        LLMConfig        `yaml:"llm"`
	Generation GenerationConfig `yaml:"generation"`
}

type ConnectionConfig struct {
	Real   RealDBConfig   `yaml:"real"`
	Shadow ShadowDBConfig `yaml:"shadow"`
}

type RealDBConfig struct {
	URL string `yaml:"url"`
}

type ShadowDBConfig struct {
	Schema string `yaml:"schema"` // schema name for synthetic data (default: autodb_shadow)
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
		Connection: ConnectionConfig{
			Shadow: ShadowDBConfig{
				Schema: "autodb_shadow",
			},
		},
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

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}
