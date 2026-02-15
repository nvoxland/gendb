package lang

// Registry maps canonical command names to their definitions.
var Registry = map[string]*CommandDef{
	"generate_data": {
		Name: "generate_data", NeedsConn: true,
		Params: []ParamDef{
			{Name: "table_name"}, {Name: "rows"}, {Name: "seed"}, {Name: "scenario"},
		},
	},
	"return_generated": {
		Name: "return_generated", NeedsConn: true,
		Params: []ParamDef{
			{Name: "table_name", Required: true}, {Name: "scenario"},
		},
	},
	"return_actual": {
		Name: "return_actual", NeedsConn: true,
		Params: []ParamDef{
			{Name: "table_name", Required: true}, {Name: "scenario"},
		},
	},
	"sync": {
		Name: "sync", NeedsConn: true,
		Params: []ParamDef{
			{Name: "table_name"}, {Name: "scenario"},
		},
	},
	"drop_scenario": {
		Name: "drop_scenario", NeedsConn: true,
		Params: []ParamDef{
			{Name: "scenario", Required: true}, {Name: "schema"},
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
