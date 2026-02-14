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
	case "mode":
		return buildMode(args)
	case "generate_data":
		return buildGenerateData(args)
	case "regenerate_data":
		return buildRegenerateData(args)
	case "reset":
		return buildReset(args)
	case "set_model":
		return buildSetModel(args)
	case "set_seed":
		return buildSetSeed(args)
	case "set_default_rows":
		return buildSetDefaultRows(args)
	case "set_column":
		return buildSetColumn(args)
	case "show_status":
		return buildShowSimple("status", args)
	case "show_tables":
		return buildShowSimple("tables", args)
	case "show_config":
		return buildShowSimple("config", args)
	case "show_profiles":
		return buildShowSimple("profiles", args)
	case "show_table":
		return buildShowTable(args)
	case "show_generation_plan":
		return buildShowGenerationPlan(args)
	case "sync_schema":
		return buildSyncSchema(args)
	case "create_profile":
		return buildCreateProfile(args)
	case "use_profile":
		return buildUseProfile(args)
	case "drop_profile":
		return buildDropProfile(args)
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

func buildMode(args map[string]string) (*Command, error) {
	mode, ok := args["mode"]
	if !ok {
		return nil, fmt.Errorf("parse error: mode() requires 'mode' argument")
	}
	cmd := &ModeCommand{Mode: strings.ToUpper(mode)}
	if tables, ok := args["tables"]; ok && tables != "" {
		cmd.Tables = strings.Split(tables, ",")
		for i := range cmd.Tables {
			cmd.Tables[i] = strings.TrimSpace(cmd.Tables[i])
		}
	}
	return &Command{Mode: cmd}, nil
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

func buildReset(args map[string]string) (*Command, error) {
	cmd := &ResetCommand{}
	if t, ok := args["table_name"]; ok {
		cmd.Table = t
	}
	return &Command{Reset: cmd}, nil
}

func buildSetModel(args map[string]string) (*Command, error) {
	name, ok := args["name"]
	if !ok {
		return nil, fmt.Errorf("parse error: set_model() requires 'name' argument")
	}
	model := &SetModel{Name: name}
	if k, ok := args["key"]; ok {
		model.Key = k
	}
	return &Command{Set: &SetCommand{Model: model}}, nil
}

func buildSetSeed(args map[string]string) (*Command, error) {
	v, ok := args["value"]
	if !ok {
		return nil, fmt.Errorf("parse error: set_seed() requires 'value' argument")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse error: invalid seed value %q", v)
	}
	return &Command{Set: &SetCommand{Seed: &n}}, nil
}

func buildSetDefaultRows(args map[string]string) (*Command, error) {
	v, ok := args["value"]
	if !ok {
		return nil, fmt.Errorf("parse error: set_default_rows() requires 'value' argument")
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("parse error: invalid default_rows value %q", v)
	}
	return &Command{Set: &SetCommand{DefaultRows: &n}}, nil
}

func buildSetColumn(args map[string]string) (*Command, error) {
	tc := &SetTableColumn{}
	if t, ok := args["table_name"]; ok {
		tc.Table = t
	}
	if c, ok := args["column_name"]; ok {
		tc.Column = c
	}
	if g, ok := args["generator"]; ok {
		tc.Generator = g
	}
	if p, ok := args["prompt"]; ok {
		tc.Prompt = p
	}
	if v, ok := args["values"]; ok && v != "" {
		tc.Values = strings.Split(v, ",")
		for i := range tc.Values {
			tc.Values[i] = strings.TrimSpace(tc.Values[i])
		}
	}
	return &Command{Set: &SetCommand{TableCol: tc}}, nil
}

func buildShowSimple(kind string, args map[string]string) (*Command, error) {
	show := &ShowCommand{}
	switch kind {
	case "status":
		show.Status = true
	case "tables":
		show.Tables = true
	case "config":
		show.Config = true
	case "profiles":
		show.Profiles = true
	}
	return &Command{Show: show}, nil
}

func buildShowTable(args map[string]string) (*Command, error) {
	table, ok := args["table_name"]
	if !ok {
		return nil, fmt.Errorf("parse error: show_table() requires 'table_name' argument")
	}
	return &Command{Show: &ShowCommand{TableInfo: &ShowTable{Table: table}}}, nil
}

func buildShowGenerationPlan(args map[string]string) (*Command, error) {
	plan := &ShowPlan{}
	if t, ok := args["table_name"]; ok {
		plan.Table = t
	}
	return &Command{Show: &ShowCommand{GenPlan: plan}}, nil
}

func buildSyncSchema(args map[string]string) (*Command, error) {
	return &Command{Sync: &SyncCommand{Schema: true}}, nil
}

func buildCreateProfile(args map[string]string) (*Command, error) {
	name, ok := args["name"]
	if !ok {
		return nil, fmt.Errorf("parse error: create_profile() requires 'name' argument")
	}
	cp := &CreateProfile{Name: name}
	if tables, ok := args["tables"]; ok && tables != "" {
		entries := strings.Split(tables, ",")
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			parts := strings.SplitN(entry, ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("parse error: invalid table entry %q, expected 'table:rows'", entry)
			}
			rows, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("parse error: invalid rows in %q", entry)
			}
			cp.Tables = append(cp.Tables, ProfileTable{
				Table: strings.TrimSpace(parts[0]),
				Rows:  rows,
			})
		}
	}
	return &Command{CreateProf: cp}, nil
}

func buildUseProfile(args map[string]string) (*Command, error) {
	name, ok := args["name"]
	if !ok {
		return nil, fmt.Errorf("parse error: use_profile() requires 'name' argument")
	}
	return &Command{UseProf: &UseProfile{Name: name}}, nil
}

func buildDropProfile(args map[string]string) (*Command, error) {
	name, ok := args["name"]
	if !ok {
		return nil, fmt.Errorf("parse error: drop_profile() requires 'name' argument")
	}
	return &Command{DropProf: &DropProfile{Name: name}}, nil
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
