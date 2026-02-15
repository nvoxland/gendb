package lang

import (
	"fmt"
	"strings"
)

// IsGenDBCommand checks if a SQL string is a CALL gendb.* command (case-insensitive).
func IsGenDBCommand(sql string) bool {
	sql = strings.TrimSpace(sql)
	prefix := "call gendb."
	if len(sql) < len(prefix) {
		return false
	}
	return strings.EqualFold(sql[:len(prefix)], prefix)
}

// Parse parses a CALL gendb.xxx(...) command string into a Command.
func Parse(input string) (*Command, error) {
	input = strings.TrimSpace(input)
	input = strings.TrimSuffix(input, ";")
	input = strings.TrimSpace(input)

	// Strip "CALL gendb." prefix (case-insensitive)
	prefix := "call gendb."
	if len(input) < len(prefix) || !strings.EqualFold(input[:len(prefix)], prefix) {
		return nil, fmt.Errorf("parse error: expected CALL gendb.* command")
	}
	rest := input[len(prefix):]

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

	// Resolve alias
	if canonical, ok := Aliases[procName]; ok {
		procName = canonical
	}

	// Registry lookup
	def, ok := Registry[procName]
	if !ok {
		return nil, fmt.Errorf("parse error: unknown procedure %q", procName)
	}

	// Validate args
	if err := validateArgs(def, args); err != nil {
		return nil, err
	}

	return &Command{Name: procName, Args: args}, nil
}

// validateArgs checks that all required params are present and no unknown params are given.
func validateArgs(def *CommandDef, args map[string]string) error {
	// Check for unknown params
	known := make(map[string]bool, len(def.Params))
	for _, p := range def.Params {
		known[p.Name] = true
	}
	for k := range args {
		if !known[k] {
			return fmt.Errorf("parse error: unknown parameter %q for %s", k, def.Name)
		}
	}

	// Check required params
	for _, p := range def.Params {
		if p.Required {
			if _, ok := args[p.Name]; !ok {
				return fmt.Errorf("parse error: %s() requires '%s' argument", def.Name, p.Name)
			}
		}
	}

	return nil
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
