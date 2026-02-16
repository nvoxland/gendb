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

	// Resolve alias
	if canonical, ok := Aliases[procName]; ok {
		procName = canonical
	}

	// Registry lookup (needed before parsing args for positional mapping)
	def, ok := Registry[procName]
	if !ok {
		return nil, fmt.Errorf("parse error: unknown procedure %q", procName)
	}

	args, err := parseCallArgs(argsStr, def.Params)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
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

// parseCallArgs parses call arguments supporting positional, named (=> or :=), and mixed notation.
// Positional args are mapped to params by order. Once a named arg appears, all subsequent must be named.
// NULL values (case-insensitive) are omitted from the map (treated as unspecified).
func parseCallArgs(argsStr string, params []ParamDef) (map[string]string, error) {
	args := make(map[string]string)
	if argsStr == "" {
		return args, nil
	}

	parts := splitArgs(argsStr)
	positionalIdx := 0
	seenNamed := false

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check for named notation: => or :=
		key, val, named := parseNamedArg(part)

		if named {
			seenNamed = true
			key = strings.ToLower(key)
			val = unquoteValue(val)
			if !strings.EqualFold(val, "NULL") {
				args[key] = val
			}
		} else {
			// Positional arg
			if seenNamed {
				return nil, fmt.Errorf("positional argument after named argument is not allowed")
			}
			if positionalIdx >= len(params) {
				return nil, fmt.Errorf("too many positional arguments (expected at most %d)", len(params))
			}
			val = unquoteValue(part)
			if !strings.EqualFold(val, "NULL") {
				args[params[positionalIdx].Name] = val
			}
			positionalIdx++
		}
	}
	return args, nil
}

// parseNamedArg checks if a part contains => or := and splits it into key/value.
// Returns the key, value, and whether it was a named arg.
func parseNamedArg(part string) (string, string, bool) {
	if idx := strings.Index(part, "=>"); idx >= 0 {
		return strings.TrimSpace(part[:idx]), strings.TrimSpace(part[idx+2:]), true
	}
	if idx := strings.Index(part, ":="); idx >= 0 {
		return strings.TrimSpace(part[:idx]), strings.TrimSpace(part[idx+2:]), true
	}
	return "", "", false
}

// unquoteValue strips surrounding single or double quotes from a value.
func unquoteValue(val string) string {
	if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
		return val[1 : len(val)-1]
	}
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		return val[1 : len(val)-1]
	}
	return val
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
