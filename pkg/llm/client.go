package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Client communicates with an OpenAI-compatible LLM API.
type Client struct {
	openai openai.Client
	model  string
}

// NewClient creates an LLM client from the given base URL, model, and optional API key.
func NewClient(baseURL, model, apiKey string) *Client {
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	} else {
		// Ollama doesn't need an API key, but the SDK requires one
		opts = append(opts, option.WithAPIKey("ollama"))
	}

	return &Client{
		openai: openai.NewClient(opts...),
		model:  model,
	}
}

// TableDataRequest describes what data to generate for a single table.
type TableDataRequest struct {
	SchemaContext      string // from FormatForLLM()
	Table              *schema.Table
	RowCount           int
	FKValues           map[string][]any  // colName -> valid FK values
	ColumnInstructions map[string]string // colName -> constraint text from config
	SkipColumns        []string          // auto-generated, serial, FK columns
	UniqueColumns      [][]string        // unique index column groups
	PreviousRows       []map[string]any  // for multi-chunk consistency
}

const chunkSize = 50

// GenerateTableData generates rows for a table via LLM, chunking if needed.
func (c *Client) GenerateTableData(ctx context.Context, req TableDataRequest) ([]map[string]any, error) {
	if req.RowCount <= chunkSize {
		return c.generateChunk(ctx, req, req.RowCount)
	}

	var allRows []map[string]any
	remaining := req.RowCount
	for remaining > 0 {
		batchSize := chunkSize
		if remaining < batchSize {
			batchSize = remaining
		}

		// Include sample of previous rows for consistency
		if len(allRows) > 0 {
			sampleSize := 5
			if len(allRows) < sampleSize {
				sampleSize = len(allRows)
			}
			req.PreviousRows = allRows[len(allRows)-sampleSize:]
		}

		rows, err := c.generateChunk(ctx, req, batchSize)
		if err != nil {
			return nil, err
		}
		allRows = append(allRows, rows...)
		remaining -= batchSize
	}

	return allRows, nil
}

func (c *Client) generateChunk(ctx context.Context, req TableDataRequest, count int) ([]map[string]any, error) {
	prompt := buildPrompt(req, count)

	resp, err := c.openai.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are generating realistic test data for a PostgreSQL database. Return ONLY a JSON array of row objects. No explanation, no markdown formatting."),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}

	content := fixJSON(stripCodeBlock(resp.Choices[0].Message.Content))

	var rows []map[string]any
	if err := json.Unmarshal([]byte(content), &rows); err != nil {
		return nil, fmt.Errorf("parsing LLM response as JSON array: %w\nContent: %s", err, content)
	}

	return rows, nil
}

func buildPrompt(req TableDataRequest, count int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Database schema context:\n%s\n", req.SchemaContext)
	fmt.Fprintf(&b, "Generate %d rows for table %q.\n\n", count, req.Table.Name)

	// List columns to generate with types
	skipSet := make(map[string]bool)
	for _, s := range req.SkipColumns {
		skipSet[s] = true
	}

	fmt.Fprintf(&b, "Columns to generate:\n")
	for _, col := range req.Table.Columns {
		if skipSet[col.Name] {
			continue
		}
		nullable := ""
		if col.IsNullable {
			nullable = " (nullable)"
		}
		fmt.Fprintf(&b, "- %s: %s%s\n", col.Name, col.DataType, nullable)
	}
	b.WriteString("\n")

	// FK value constraints
	if len(req.FKValues) > 0 {
		fmt.Fprintf(&b, "Foreign key values (you MUST pick from these lists):\n")
		for col, vals := range req.FKValues {
			strs := make([]string, len(vals))
			for i, v := range vals {
				strs[i] = fmt.Sprintf("%v", v)
			}
			// Limit display to 50 values
			display := strs
			if len(display) > 50 {
				display = display[:50]
			}
			fmt.Fprintf(&b, "- %s: [%s]\n", col, strings.Join(display, ", "))
		}
		b.WriteString("\n")
	}

	// Unique constraints
	if len(req.UniqueColumns) > 0 {
		fmt.Fprintf(&b, "Unique constraints (values must be unique across all rows):\n")
		for _, cols := range req.UniqueColumns {
			fmt.Fprintf(&b, "- UNIQUE(%s)\n", strings.Join(cols, ", "))
		}
		b.WriteString("\n")
	}

	// Check constraints
	if len(req.Table.Checks) > 0 {
		fmt.Fprintf(&b, "Check constraints:\n")
		for _, ck := range req.Table.Checks {
			fmt.Fprintf(&b, "- %s\n", ck.Expression)
		}
		b.WriteString("\n")
	}

	// Config instructions
	if len(req.ColumnInstructions) > 0 {
		fmt.Fprintf(&b, "Column instructions:\n")
		for col, instr := range req.ColumnInstructions {
			fmt.Fprintf(&b, "- %s: %s\n", col, instr)
		}
		b.WriteString("\n")
	}

	// Previous rows for consistency
	if len(req.PreviousRows) > 0 {
		sample, _ := json.Marshal(req.PreviousRows)
		fmt.Fprintf(&b, "Previous rows (maintain consistency in style and values):\n%s\n\n", string(sample))
	}

	fmt.Fprintf(&b, "Return ONLY a JSON array of %d row objects. Each object should have keys matching the column names above. Respect data types and constraints. Generate realistic, semantically coherent data.", count)

	return b.String()
}

// GenerateColumnValues generates values for a single column across multiple rows.
func (c *Client) GenerateColumnValues(ctx context.Context, schemaContext string, table *schema.Table, col *schema.Column, count int) ([]any, error) {
	prompt := fmt.Sprintf(
		"Database schema context:\n%s\nGenerate %d realistic values for column %q (type: %s) in table %q.\nReturn ONLY a JSON array of values. No explanation.",
		schemaContext, count, col.Name, col.DataType, table.Name,
	)

	resp, err := c.openai.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are generating realistic test data for a PostgreSQL database. Return ONLY a JSON array of values."),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no choices")
	}

	content := fixJSON(stripCodeBlock(resp.Choices[0].Message.Content))

	var values []any
	if err := json.Unmarshal([]byte(content), &values); err != nil {
		return nil, fmt.Errorf("parsing LLM response: %w\nContent: %s", err, content)
	}

	return values, nil
}

// fixJSON attempts to fix common LLM JSON issues, such as single-quoted strings.
func fixJSON(s string) string {
	// If it already parses, return as-is
	if json.Valid([]byte(s)) {
		return s
	}

	// Replace single-quoted strings with double-quoted strings.
	// This handles the common case where LLMs return Python-style dicts.
	var b strings.Builder
	inDouble := false
	inSingle := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\' && i+1 < len(s) && (inDouble || inSingle):
			b.WriteByte(ch)
			i++
			b.WriteByte(s[i])
		case ch == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteByte(ch)
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteByte('"')
		case ch == '"' && inSingle:
			// Escape double quotes inside what was a single-quoted string
			b.WriteString(`\"`)
		default:
			b.WriteByte(ch)
		}
	}

	result := b.String()
	// Also replace Python True/False/None with JSON equivalents
	result = strings.ReplaceAll(result, ": True", ": true")
	result = strings.ReplaceAll(result, ": False", ": false")
	result = strings.ReplaceAll(result, ": None", ": null")

	return result
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
