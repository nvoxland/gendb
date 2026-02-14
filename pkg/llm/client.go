package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// GenerationPlan describes how to generate data for each column.
type GenerationPlan struct {
	Tables map[string]TablePlan `json:"tables"`
}

// TablePlan describes generation for a single table.
type TablePlan struct {
	Columns map[string]ColumnPlan `json:"columns"`
}

// ColumnPlan describes how to generate data for a single column.
type ColumnPlan struct {
	Generator string            `json:"generator"`
	Template  string            `json:"template,omitempty"`
	Values    []string          `json:"values,omitempty"`
	Format    string            `json:"format,omitempty"`
	Min       *float64          `json:"min,omitempty"`
	Max       *float64          `json:"max,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

// Client communicates with an OpenAI-compatible LLM API.
type Client struct {
	openai openai.Client
	model  string
}

// NewClient creates an LLM client from the given config.
func NewClient(cfg config.LLMConfig) *Client {
	opts := []option.RequestOption{
		option.WithBaseURL(cfg.BaseURL),
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	} else {
		// Ollama doesn't need an API key, but the SDK requires one
		opts = append(opts, option.WithAPIKey("ollama"))
	}

	return &Client{
		openai: openai.NewClient(opts...),
		model:  cfg.Model,
	}
}

const analyzePrompt = `You are a database expert. Given a database schema, produce a JSON generation plan that maps each column to an appropriate fake data generator.

Available generators:
- "person.first_name", "person.last_name", "person.full_name" — name generators
- "internet.email" — email addresses (supports "template" field like "{first_name}.{last_name}@example.com")
- "internet.url", "internet.image_url", "internet.domain" — URL generators
- "phone.national", "phone.international" — phone numbers
- "address.street", "address.city", "address.state", "address.zip", "address.country" — address parts
- "company.name", "company.bs", "company.suffix" — company data
- "lorem.sentence", "lorem.paragraph" — text generators (supports "params": {"sentences": "3"})
- "time.recent" — recent timestamps (supports "params": {"days": "365"})
- "time.past", "time.future" — timestamp generators
- "number.int" — integer (supports "min" and "max")
- "number.float" — float (supports "min" and "max")
- "number.price" — price values
- "uuid" — UUID v4
- "boolean" — true/false
- "one_of" — random selection from "values" list
- "regex" — matches a pattern in "format" field
- "sequence" — auto-incrementing integer (for serial/identity columns)
- "skip" — skip this column (has a default value or is generated)

For each table and column, choose the most semantically appropriate generator based on the column name, type, and any constraints.

IMPORTANT:
- Columns with DEFAULT values that look auto-generated (sequences, now(), gen_random_uuid()) should use "skip"
- Serial/identity/auto-increment columns should use "skip"
- For columns with CHECK constraints limiting values, use "one_of" with the allowed values
- Foreign key columns should use "skip" (they'll be filled by the FK resolver)
- Consider column names semantically: "email" → internet.email, "first_name" → person.first_name, etc.

Return ONLY valid JSON with this structure:
{
  "tables": {
    "table_name": {
      "columns": {
        "column_name": {"generator": "...", ...}
      }
    }
  }
}`

// AnalyzeSchema sends the schema to the LLM and gets back a generation plan.
func (c *Client) AnalyzeSchema(ctx context.Context, sg *schema.SchemaGraph) (*GenerationPlan, error) {
	schemaText := sg.FormatForLLM()

	resp, err := c.openai.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(analyzePrompt),
			openai.UserMessage("Here is the database schema:\n\n" + schemaText),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}

	content := resp.Choices[0].Message.Content
	plan, err := parseGenerationPlan(content)
	if err != nil {
		return nil, fmt.Errorf("parsing LLM response: %w", err)
	}

	return plan, nil
}

// GenerateText generates a single text value using the LLM.
func (c *Client) GenerateText(ctx context.Context, prompt string, vars map[string]string) (string, error) {
	// Substitute variables in the prompt
	expandedPrompt := prompt
	for k, v := range vars {
		expandedPrompt = replaceVar(expandedPrompt, k, v)
	}

	resp, err := c.openai.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("Generate exactly one value. Return only the value, no explanation or formatting."),
			openai.UserMessage(expandedPrompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("LLM text generation failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	return resp.Choices[0].Message.Content, nil
}

func replaceVar(s, key, value string) string {
	return strings.ReplaceAll(s, "{"+key+"}", value)
}

func parseGenerationPlan(content string) (*GenerationPlan, error) {
	// The LLM might wrap JSON in markdown code blocks
	content = stripCodeBlock(content)

	var plan GenerationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w\nContent: %s", err, content)
	}
	return &plan, nil
}

func stripCodeBlock(s string) string {
	// Remove ```json ... ``` wrapper if present
	if len(s) > 7 && s[:7] == "```json" {
		s = s[7:]
	} else if len(s) > 3 && s[:3] == "```" {
		s = s[3:]
	}
	if len(s) > 3 && s[len(s)-3:] == "```" {
		s = s[:len(s)-3]
	}
	// Trim whitespace
	for len(s) > 0 && (s[0] == '\n' || s[0] == '\r' || s[0] == ' ') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
