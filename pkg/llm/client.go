package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

// Client communicates with an OpenAI-compatible LLM API.
type Client struct {
	openai           openai.Client
	model            string
	temperature      *float64
	structuredOutput bool
	provider         string // "ollama" | "openai" | "custom"
	chunkSize        int
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithTemperature sets the sampling temperature.
func WithTemperature(t *float64) ClientOption {
	return func(c *Client) { c.temperature = t }
}

// WithStructuredOutput enables JSON Schema structured outputs.
func WithStructuredOutput(enabled bool) ClientOption {
	return func(c *Client) { c.structuredOutput = enabled }
}

// WithProvider sets the provider name for compatibility adjustments.
func WithProvider(provider string) ClientOption {
	return func(c *Client) { c.provider = provider }
}

// WithChunkSize sets the number of rows per LLM request.
func WithChunkSize(size int) ClientOption {
	return func(c *Client) { c.chunkSize = size }
}

// NewClient creates an LLM client from the given base URL, model, and optional API key.
func NewClient(baseURL, model, apiKey string, opts ...ClientOption) *Client {
	reqOpts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	} else {
		// Ollama doesn't need an API key, but the SDK requires one
		reqOpts = append(reqOpts, option.WithAPIKey("ollama"))
	}

	c := &Client{
		openai:    openai.NewClient(reqOpts...),
		model:     model,
		chunkSize: defaultChunkSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.chunkSize <= 0 {
		c.chunkSize = defaultChunkSize
	}
	return c
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

const defaultChunkSize = 50

// GenerateTableData generates rows for a table via LLM, chunking if needed.
func (c *Client) GenerateTableData(ctx context.Context, req TableDataRequest) ([]map[string]any, error) {
	cs := c.chunkSize
	if req.RowCount <= cs {
		slog.Debug("Requesting LLM data (single chunk)", "table", req.Table.Name, "rows", req.RowCount)
		return c.generateChunk(ctx, req, req.RowCount)
	}

	slog.Debug("Requesting LLM data (chunked)", "table", req.Table.Name, "rows", req.RowCount, "chunk_size", cs)
	var allRows []map[string]any
	remaining := req.RowCount
	chunk := 0
	for remaining > 0 {
		chunk++
		batchSize := cs
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

		slog.Debug("Generating chunk", "table", req.Table.Name, "chunk", chunk, "batch_size", batchSize, "remaining", remaining)
		rows, err := c.generateChunk(ctx, req, batchSize)
		if err != nil {
			return nil, err
		}
		allRows = append(allRows, rows...)
		remaining -= batchSize
	}

	slog.Debug("LLM generation complete", "table", req.Table.Name, "total_rows", len(allRows))
	return allRows, nil
}

const systemPrompt = `You are a synthetic data generator for a PostgreSQL database.

Guidelines:
- Return ONLY a valid JSON array of row objects. No explanation, no markdown, no code blocks.
- Use realistic values: real-sounding names, plausible addresses, valid email formats, sensible dates.
- Vary the data: avoid repeating the same patterns. Mix lengths, styles, and distributions naturally.
- Respect all data types exactly. Strings must fit within any specified length limits (e.g., varchar(50) means max 50 characters).
- Respect all CHECK, UNIQUE, and NOT NULL constraints.
- For nullable columns, include some null values (roughly 5-15%) unless instructed otherwise.
- Foreign key columns must use only the provided valid values.
- Generate semantically coherent rows: related columns within a row should make sense together.`

const maxRetries = 2

func (c *Client) generateChunk(ctx context.Context, req TableDataRequest, count int) ([]map[string]any, error) {
	prompt := buildPrompt(req, count)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(prompt),
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			slog.Warn("Retrying LLM request after JSON parse failure", "table", req.Table.Name, "attempt", attempt, "error", lastErr)
			messages = append(messages, openai.UserMessage(
				fmt.Sprintf("Your previous response was not valid JSON. Error: %s. Please return ONLY a valid JSON array of %d row objects.", lastErr, count),
			))
		}

		slog.Debug("Sending LLM request", "model", c.model, "table", req.Table.Name, "requested_rows", count, "attempt", attempt)

		params := openai.ChatCompletionNewParams{
			Model:    c.model,
			Messages: messages,
		}
		if c.temperature != nil {
			params.Temperature = param.NewOpt(*c.temperature)
		}
		if c.structuredOutput {
			params.ResponseFormat = c.buildResponseFormat(req)
		}

		resp, err := c.openai.Chat.Completions.New(ctx, params)
		if err != nil {
			slog.Error("LLM API call failed", "model", c.model, "table", req.Table.Name, "error", err)
			return nil, fmt.Errorf("LLM API call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			slog.Error("LLM returned no choices", "model", c.model, "table", req.Table.Name)
			return nil, fmt.Errorf("LLM returned no choices")
		}

		slog.Debug("LLM response received", "table", req.Table.Name, "response_length", len(resp.Choices[0].Message.Content))
		content := resp.Choices[0].Message.Content

		rows, parseErr := c.parseResponse(content, c.structuredOutput)
		if parseErr != nil {
			lastErr = parseErr
			// Add the assistant's bad response to conversation for context
			messages = append(messages, openai.AssistantMessage(content))
			continue
		}

		slog.Debug("Parsed LLM response", "table", req.Table.Name, "parsed_rows", len(rows))
		return rows, nil
	}

	slog.Error("Failed to parse LLM response after retries", "table", req.Table.Name, "error", lastErr)
	return nil, fmt.Errorf("parsing LLM response as JSON array after %d retries: %w", maxRetries, lastErr)
}

// parseResponse parses the LLM response content into rows.
func (c *Client) parseResponse(content string, structured bool) ([]map[string]any, error) {
	if structured {
		// Structured output wraps in {"rows": [...]}
		var wrapper struct {
			Rows []map[string]any `json:"rows"`
		}
		if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
			return nil, fmt.Errorf("parsing structured response: %w\nContent: %s", err, content)
		}
		return wrapper.Rows, nil
	}

	cleaned := fixJSON(stripCodeBlock(content))
	var rows []map[string]any
	if err := json.Unmarshal([]byte(cleaned), &rows); err == nil {
		return rows, nil
	}

	// Fallback: try parsing as []any and recover stringified objects
	if recovered := recoverStringifiedObjects(cleaned); len(recovered) > 0 {
		slog.Warn("Recovered rows from stringified JSON objects", "recovered_rows", len(recovered))
		return recovered, nil
	}

	// Last resort: extract individual {...} objects via brace matching
	if extracted := extractJSONObjects(cleaned); len(extracted) > 0 {
		slog.Warn("Extracted rows via brace-matching fallback", "extracted_rows", len(extracted))
		return extracted, nil
	}

	return nil, fmt.Errorf("parsing response as JSON array (all fallbacks failed)\nContent: %s", cleaned)
}

// buildResponseFormat constructs a JSON Schema response format for structured outputs.
func (c *Client) buildResponseFormat(req TableDataRequest) openai.ChatCompletionNewParamsResponseFormatUnion {
	skipSet := make(map[string]bool)
	for _, s := range req.SkipColumns {
		skipSet[s] = true
	}

	properties := make(map[string]any)
	var required []string
	for _, col := range req.Table.Columns {
		if skipSet[col.Name] {
			continue
		}
		prop := buildColumnSchema(col)
		properties[col.Name] = prop
		if !col.IsNullable {
			required = append(required, col.Name)
		}
	}
	// Also include FK columns
	for colName := range req.FKValues {
		if _, exists := properties[colName]; !exists {
			col := req.Table.ColumnByName(colName)
			if col != nil {
				properties[colName] = buildColumnSchema(col)
				if !col.IsNullable {
					required = append(required, colName)
				}
			}
		}
	}

	rowSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		rowSchema["required"] = required
	}

	topSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rows": map[string]any{
				"type":  "array",
				"items": rowSchema,
			},
		},
		"required":             []string{"rows"},
		"additionalProperties": false,
	}

	jsonSchema := shared.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:   "table_rows",
		Schema: topSchema,
	}
	// OpenAI supports strict mode; Ollama does not
	if c.provider == "openai" {
		jsonSchema.Strict = param.NewOpt(true)
	}

	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
			JSONSchema: jsonSchema,
		},
	}
}

var varcharLenRegex = regexp.MustCompile(`\((\d+)\)`)

// buildColumnSchema maps a PostgreSQL column to a JSON Schema type.
func buildColumnSchema(col *schema.Column) map[string]any {
	dt := strings.ToLower(col.DataType)
	prop := make(map[string]any)

	var jsonType string
	switch {
	case strings.Contains(dt, "int") || strings.Contains(dt, "serial"):
		jsonType = "integer"
	case strings.Contains(dt, "float") || strings.Contains(dt, "double") || strings.Contains(dt, "real") ||
		strings.Contains(dt, "numeric") || strings.Contains(dt, "decimal"):
		jsonType = "number"
	case strings.Contains(dt, "bool"):
		jsonType = "boolean"
	default:
		jsonType = "string"
	}

	if col.IsNullable {
		prop["type"] = []string{jsonType, "null"}
	} else {
		prop["type"] = jsonType
	}

	// Add maxLength for varchar types
	if strings.Contains(dt, "varchar") || strings.Contains(dt, "character varying") {
		if m := varcharLenRegex.FindStringSubmatch(col.DataType); len(m) == 2 {
			if maxLen, err := strconv.Atoi(m[1]); err == nil {
				prop["maxLength"] = maxLen
			}
		}
	}

	return prop
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
	var exampleCols []*schema.Column
	for _, col := range req.Table.Columns {
		if skipSet[col.Name] {
			continue
		}
		nullable := ""
		if col.IsNullable {
			nullable = " (nullable)"
		}
		fmt.Fprintf(&b, "- %s: %s%s\n", col.Name, col.DataType, nullable)
		exampleCols = append(exampleCols, col)
	}
	b.WriteString("\n")

	// Few-shot example row
	if len(exampleCols) > 0 {
		fmt.Fprintf(&b, "Example format (generate NEW values, not these):\n")
		example := buildExampleRow(exampleCols)
		exampleJSON, _ := json.Marshal([]map[string]any{example})
		fmt.Fprintf(&b, "%s\n\n", string(exampleJSON))
	}

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
		fmt.Fprintf(&b, "CHECK CONSTRAINTS (rows MUST satisfy ALL of these):\n")
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

// buildExampleRow creates a single example row with type-appropriate placeholder values.
func buildExampleRow(cols []*schema.Column) map[string]any {
	row := make(map[string]any)
	for _, col := range cols {
		dt := strings.ToLower(col.DataType)
		switch {
		case strings.Contains(dt, "int") || strings.Contains(dt, "serial"):
			row[col.Name] = 1
		case strings.Contains(dt, "float") || strings.Contains(dt, "double") || strings.Contains(dt, "real") ||
			strings.Contains(dt, "numeric") || strings.Contains(dt, "decimal"):
			row[col.Name] = 1.0
		case strings.Contains(dt, "bool"):
			row[col.Name] = true
		case strings.Contains(dt, "timestamp"):
			row[col.Name] = "2024-01-15T10:30:00Z"
		case strings.Contains(dt, "date"):
			row[col.Name] = "2024-01-15"
		case strings.Contains(dt, "uuid"):
			row[col.Name] = "550e8400-e29b-41d4-a716-446655440000"
		case strings.Contains(dt, "json"):
			row[col.Name] = "{}"
		default:
			row[col.Name] = "example_" + col.Name
		}
	}
	return row
}

// GenerateColumnValues generates values for a single column across multiple rows.
func (c *Client) GenerateColumnValues(ctx context.Context, schemaContext string, table *schema.Table, col *schema.Column, count int) ([]any, error) {
	slog.Debug("Generating column values via LLM", "table", table.Name, "column", col.Name, "count", count)

	prompt := fmt.Sprintf(
		"Database schema context:\n%s\nGenerate %d realistic values for column %q (type: %s) in table %q.\nReturn ONLY a JSON array of values. No explanation.",
		schemaContext, count, col.Name, col.DataType, table.Name,
	)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are generating realistic test data for a PostgreSQL database. Return ONLY a JSON array of values."),
		openai.UserMessage(prompt),
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			slog.Warn("Retrying LLM column values request after parse failure", "table", table.Name, "column", col.Name, "attempt", attempt, "error", lastErr)
			messages = append(messages, openai.UserMessage(
				fmt.Sprintf("Your previous response was not valid JSON. Error: %s. Please return ONLY a valid JSON array of %d values.", lastErr, count),
			))
		}

		resp, err := c.openai.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    c.model,
			Messages: messages,
		})
		if err != nil {
			slog.Error("LLM API call failed for column values", "table", table.Name, "column", col.Name, "error", err)
			return nil, fmt.Errorf("LLM API call failed: %w", err)
		}

		if len(resp.Choices) == 0 {
			slog.Error("LLM returned no choices for column values", "table", table.Name, "column", col.Name)
			return nil, fmt.Errorf("LLM returned no choices")
		}

		content := resp.Choices[0].Message.Content
		cleaned := fixJSON(stripCodeBlock(content))

		values, parseErr := parseColumnValues(cleaned)
		if parseErr != nil {
			lastErr = parseErr
			messages = append(messages, openai.AssistantMessage(content))
			continue
		}

		slog.Debug("Generated column values", "table", table.Name, "column", col.Name, "values", len(values))
		return values, nil
	}

	slog.Error("Failed to parse LLM column values after retries", "table", table.Name, "column", col.Name, "error", lastErr)
	return nil, fmt.Errorf("parsing LLM column values after %d retries: %w", maxRetries, lastErr)
}

// parseColumnValues attempts to parse a JSON array of values with fallbacks.
func parseColumnValues(s string) ([]any, error) {
	var values []any
	if err := json.Unmarshal([]byte(s), &values); err == nil {
		return values, nil
	}

	// Fallback: extract JSON objects and convert to []any
	if objects := extractJSONObjects(s); len(objects) > 0 {
		slog.Warn("Extracted column values via brace-matching fallback", "extracted", len(objects))
		result := make([]any, len(objects))
		for i, obj := range objects {
			result[i] = obj
		}
		return result, nil
	}

	return nil, fmt.Errorf("parsing column values as JSON array (all fallbacks failed)\nContent: %s", s)
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

	// Remove trailing commas before ] or } (outside strings)
	result = removeTrailingCommas(result)

	return result
}

// removeTrailingCommas removes trailing/double commas outside of strings.
func removeTrailingCommas(s string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\\' && inStr && i+1 < len(s) {
			b.WriteByte(ch)
			i++
			b.WriteByte(s[i])
			continue
		}
		if ch == '"' {
			inStr = !inStr
			b.WriteByte(ch)
			continue
		}
		if inStr {
			b.WriteByte(ch)
			continue
		}
		if ch == ',' {
			// Look ahead past whitespace for ] or } or another comma
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == ']' || s[j] == '}') {
				// Trailing comma — skip it
				continue
			}
			if j < len(s) && s[j] == ',' {
				// Double comma — skip this one
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// recoverStringifiedObjects attempts to parse a JSON array where elements
// may be stringified JSON objects (e.g., ["{\"id\":1}", {"id":2}]).
func recoverStringifiedObjects(s string) []map[string]any {
	var elements []any
	if err := json.Unmarshal([]byte(s), &elements); err != nil {
		return nil
	}

	var rows []map[string]any
	for _, elem := range elements {
		switch v := elem.(type) {
		case map[string]any:
			rows = append(rows, v)
		case string:
			var obj map[string]any
			if err := json.Unmarshal([]byte(v), &obj); err == nil {
				rows = append(rows, obj)
			}
		}
	}
	return rows
}

// extractJSONObjects scans a string for {...} substrings using brace matching
// and returns any that parse as valid JSON objects.
func extractJSONObjects(s string) []map[string]any {
	var results []map[string]any
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		depth := 0
		inStr := false
		for j := i; j < len(s); j++ {
			ch := s[j]
			if ch == '\\' && inStr {
				j++ // skip escaped char
				continue
			}
			if ch == '"' {
				inStr = !inStr
				continue
			}
			if inStr {
				continue
			}
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					candidate := s[i : j+1]
					var obj map[string]any
					if err := json.Unmarshal([]byte(candidate), &obj); err == nil {
						results = append(results, obj)
					}
					i = j // outer loop will i++
					break
				}
			}
		}
	}
	return results
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
