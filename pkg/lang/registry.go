package lang

// Registry maps canonical command names to their definitions.
var Registry = map[string]*CommandDef{
	"generate_data": {
		Name: "generate_data", NeedsConn: true,
		Comment: "Generate synthetic data into the synthetic schema.",
		Params: []ParamDef{
			{Name: "include", SQLType: "text", Default: "NULL"},
			{Name: "exclude", SQLType: "text", Default: "NULL"},
			{Name: "rows", SQLType: "integer", Default: "100"},
			{Name: "seed", SQLType: "bigint", Default: "NULL"},
			{Name: "scenario", SQLType: "text", Default: "'default'"},
			{Name: "include_sample_data", SQLType: "boolean", Default: "true"},
			{Name: "prompt", SQLType: "text", Default: "NULL"},
		},
	},
	"return_generated": {
		Name: "return_generated", NeedsConn: true,
		Comment: "Route queries for one or more tables to generated (synthetic) data.",
		Params: []ParamDef{
			{Name: "include", SQLType: "text", Default: "NULL"},
			{Name: "exclude", SQLType: "text", Default: "NULL"},
			{Name: "scenario", SQLType: "text", Default: "'default'"},
		},
	},
	"return_actual": {
		Name: "return_actual", NeedsConn: true,
		Comment: "Restore one or more tables to return real data.",
		Params: []ParamDef{
			{Name: "include", SQLType: "text", Default: "NULL"},
			{Name: "exclude", SQLType: "text", Default: "NULL"},
			{Name: "scenario", SQLType: "text", Default: "'default'"},
		},
	},
	"sync": {
		Name: "sync", NeedsConn: true,
		Comment: "Sync synthetic table schemas with their original tables.",
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
var Aliases = map[string]string{}

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
