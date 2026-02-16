package lang

// Registry maps canonical command names to their definitions.
var Registry = map[string]*CommandDef{
	"generate_data": {
		Name: "generate_data", NeedsConn: true,
		Comment: "Generate synthetic data into the shadow schema.",
		Params: []ParamDef{
			{Name: "table_pattern", SQLType: "text", Default: "NULL"},
			{Name: "rows", SQLType: "integer", Default: "100"},
			{Name: "seed", SQLType: "bigint", Default: "NULL"},
			{Name: "scenario", SQLType: "text", Default: "'default'"},
		},
	},
	"return_generated": {
		Name: "return_generated", NeedsConn: true,
		Comment: "Route queries for a table to generated (shadow) data.",
		Params: []ParamDef{
			{Name: "table_name", Required: true, SQLType: "text"},
			{Name: "scenario", SQLType: "text", Default: "'default'"},
		},
	},
	"return_actual": {
		Name: "return_actual", NeedsConn: true,
		Comment: "Restore a table to return real data.",
		Params: []ParamDef{
			{Name: "table_name", Required: true, SQLType: "text"},
			{Name: "scenario", SQLType: "text", Default: "'default'"},
		},
	},
	"sync": {
		Name: "sync", NeedsConn: true,
		Comment: "Sync shadow table schemas with their original tables.",
		Params: []ParamDef{
			{Name: "table_name", SQLType: "text", Default: "NULL"},
			{Name: "scenario", SQLType: "text", Default: "NULL"},
		},
	},
	"drop_scenario": {
		Name: "drop_scenario", NeedsConn: true,
		Comment: "Drop all generated tables for a scenario.",
		Params: []ParamDef{
			{Name: "scenario", Required: true, SQLType: "text"},
			{Name: "schema", SQLType: "text", Default: "NULL"},
		},
	},
}

// Aliases maps alternative command names to their canonical names.
var Aliases = map[string]string{
	"regenerate_data": "generate_data",
}

// RegisterHandler sets the handler function for a registered command.
func RegisterHandler(name string, handler HandlerFunc) {
	Registry[name].Handler = handler
}

// ResetHandlers clears all handler functions (useful for tests).
func ResetHandlers() {
	for _, def := range Registry {
		def.Handler = nil
	}
}
