package lang

import "context"

// Command is the unified AST node for all GenDB commands.
type Command struct {
	Name string            // canonical name, e.g. "generate_data"
	Args map[string]string // validated params, e.g. {"table_name": "users", "rows": "500"}
}

// NeedsConn reports whether this command requires a database connection.
func (c *Command) NeedsConn() bool {
	if def, ok := Registry[c.Name]; ok {
		return def.NeedsConn
	}
	return false
}

// ParamDef describes a single parameter accepted by a command.
type ParamDef struct {
	Name     string
	Required bool
	SQLType  string // e.g. "text", "integer", "bigint" — defaults to "text"
	Default  string // SQL default literal, e.g. "'default'", "NULL", "100"
	Comment  string // for COMMENT ON PROCEDURE
}

// CommandDef is the registry entry for a GenDB command.
type CommandDef struct {
	Name      string
	Comment   string // procedure-level comment
	Params    []ParamDef
	NeedsConn bool
	Handler   HandlerFunc
}

// HandlerFunc is the signature for command handlers.
type HandlerFunc func(ctx context.Context, args map[string]string) (*Result, error)

// Result represents the output of a GenDB command execution.
type Result struct {
	Tag     string     // command completion tag, e.g. "GENDB GENERATE DATA FOR users ROWS 500"
	Columns []string   // column names for tabular results
	Rows    [][]string // row data for tabular results
}
