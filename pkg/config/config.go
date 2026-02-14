package config

// Config holds the AutoDB configuration.
type Config struct {
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
