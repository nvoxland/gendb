package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// IsAutoDBCommand checks if a SQL string is a CALL autodb.* command (case-insensitive).
func IsAutoDBCommand(sql string) bool {
	sql = strings.TrimSpace(sql)
	if len(sql) < len("CALL autodb.") {
		return false
	}
	return strings.EqualFold(sql[:12], "call autodb.")
}

// Parse parses a CALL autodb.xxx(...) command string into an AST.
func Parse(input string) (*Command, error) {
	input = strings.TrimSpace(input)
	input = strings.TrimSuffix(input, ";")
	input = strings.TrimSpace(input)

	// Strip "CALL autodb." prefix (case-insensitive)
	if len(input) < 12 || !strings.EqualFold(input[:12], "call autodb.") {
		return nil, fmt.Errorf("parse error: expected CALL autodb.* command")
	}
	rest := input[12:]

	// Extract procedure name and args
	parenIdx := strings.Index(rest, "(")
	if parenIdx < 0 {
		return nil, fmt.Errorf("parse error: expected '(' after procedure name")
	}
	procName := strings.TrimSpace(rest[:parenIdx])
	procName = strings.ToLower(procName)

	// Extract args between parens
	if rest[len(rest)-1] != ')' {
		return nil, fmt.Errorf("parse error: expected ')' at end of command")
	}
	argsStr := strings.TrimSpace(rest[parenIdx+1 : len(rest)-1])

	args, err := parseCallArgs(argsStr)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	switch procName {
	case "generate_data":
		return buildGenerateData(args)
	case "regenerate_data":
		return buildRegenerateData(args)
	case "return_generated":
		return buildReturnGenerated(args)
	case "return_actual":
		return buildReturnActual(args)
	default:
		return nil, fmt.Errorf("parse error: unknown procedure %q", procName)
	}
}

// parseCallArgs parses "key => value, key2 => value2" into a map.
// Values may be quoted with single quotes. Empty string returns empty map.
func parseCallArgs(argsStr string) (map[string]string, error) {
	args := make(map[string]string)
	if argsStr == "" {
		return args, nil
	}

	// Split on commas, but respect single-quoted strings
	parts := splitArgs(argsStr)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, "=>")
		if idx < 0 {
			return nil, fmt.Errorf("expected '=>' in argument %q", part)
		}
		key := strings.TrimSpace(part[:idx])
		key = strings.ToLower(key)
		val := strings.TrimSpace(part[idx+2:])
		// Unquote single-quoted strings
		if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
			val = val[1 : len(val)-1]
		}
		// Unquote double-quoted strings
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		args[key] = val
	}
	return args, nil
}

// splitArgs splits on commas that are not inside single or double quotes.
func splitArgs(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune
	for _, ch := range s {
		if !inQuote && (ch == '\'' || ch == '"') {
			inQuote = true
			quoteChar = ch
			current.WriteRune(ch)
		} else if inQuote && ch == quoteChar {
			inQuote = false
			current.WriteRune(ch)
		} else if ch == ',' && !inQuote {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func buildGenerateData(args map[string]string) (*Command, error) {
	cmd := &GenerateCommand{}
	if t, ok := args["table_name"]; ok {
		cmd.Table = t
	}
	if r, ok := args["rows"]; ok {
		n, err := strconv.Atoi(r)
		if err != nil {
			return nil, fmt.Errorf("parse error: invalid rows value %q", r)
		}
		cmd.Rows = n
	}
	if s, ok := args["seed"]; ok {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse error: invalid seed value %q", s)
		}
		cmd.Seed = &n
	}
	return &Command{Generate: cmd}, nil
}

func buildRegenerateData(args map[string]string) (*Command, error) {
	return buildGenerateData(args)
}

func buildReturnGenerated(args map[string]string) (*Command, error) {
	table, ok := args["table_name"]
	if !ok {
		return nil, fmt.Errorf("parse error: return_generated() requires 'table_name' argument")
	}
	return &Command{ReturnGenerated: &ReturnGeneratedCommand{Table: table}}, nil
}

func buildReturnActual(args map[string]string) (*Command, error) {
	table, ok := args["table_name"]
	if !ok {
		return nil, fmt.Errorf("parse error: return_actual() requires 'table_name' argument")
	}
	return &Command{ReturnActual: &ReturnActualCommand{Table: table}}, nil
}
