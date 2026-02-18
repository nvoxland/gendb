package lang

import (
	"fmt"
	"sort"
	"strings"
)

// BuildSetupSQL generates the full SQL to create the gendb schema, stub
// procedures, comments, and supporting tables from the Registry and Aliases.
func BuildSetupSQL() string {
	var b strings.Builder

	b.WriteString("CREATE SCHEMA IF NOT EXISTS gendb;\n")

	// Collect all procedure names in deterministic order.
	// Each entry is {name, *CommandDef}.
	type procEntry struct {
		name string
		def  *CommandDef
	}
	var procs []procEntry

	// Registry commands
	names := make([]string, 0, len(Registry))
	for name := range Registry {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		procs = append(procs, procEntry{name, Registry[name]})
	}

	// Aliases — resolve to the canonical CommandDef
	aliasNames := make([]string, 0, len(Aliases))
	for alias := range Aliases {
		aliasNames = append(aliasNames, alias)
	}
	sort.Strings(aliasNames)
	for _, alias := range aliasNames {
		canonical := Aliases[alias]
		procs = append(procs, procEntry{alias, Registry[canonical]})
	}

	// Emit DROP + CREATE PROCEDURE and COMMENT for each entry.
	// DROP is needed because CREATE OR REPLACE cannot rename parameters.
	for _, p := range procs {
		b.WriteString("\n")
		b.WriteString(buildDropProcedure(p.name, p.def))
		b.WriteString(buildCreateProcedure(p.name, p.def))

		comment := p.def.Comment
		if _, isAlias := Aliases[p.name]; isAlias {
			comment = fmt.Sprintf("Alias for %s.", Aliases[p.name])
		}
		if comment != "" {
			b.WriteString(fmt.Sprintf("\nCOMMENT ON PROCEDURE gendb.%s IS '%s';\n", p.name, comment))
		}
	}

	// Permanent view that reads per-session GUC variables set by BuildInfoSQL.
	b.WriteString("\nCREATE OR REPLACE VIEW gendb.info AS SELECT")
	b.WriteString(" current_setting('gendb.info.version', true) AS version,")
	b.WriteString(" current_setting('gendb.info.model', true) AS model,")
	b.WriteString(" current_setting('gendb.info.provider', true) AS provider,")
	b.WriteString(" current_setting('gendb.info.structured_output', true)::boolean AS structured_output;\n")
	b.WriteString("COMMENT ON VIEW gendb.info IS 'GenDB proxy instance information (version, LLM configuration).';\n")

	// History table for tracking operations.
	b.WriteString("\nCREATE TABLE IF NOT EXISTS gendb.history (\n")
	b.WriteString("    id BIGSERIAL PRIMARY KEY,\n")
	b.WriteString("    executed_at TIMESTAMPTZ NOT NULL DEFAULT now(),\n")
	b.WriteString("    operation TEXT NOT NULL,\n")
	b.WriteString("    scenario TEXT,\n")
	b.WriteString("    details JSONB,\n")
	b.WriteString("    status TEXT NOT NULL,\n")
	b.WriteString("    error_message TEXT,\n")
	b.WriteString("    duration_ms BIGINT\n")
	b.WriteString(");\n")
	b.WriteString("COMMENT ON TABLE gendb.history IS 'Tracks operations performed by GenDB (generate_data, sync, drop_scenario, etc.).';\n")

	return b.String()
}

// BuildInfoSQL returns SQL that sets session-scoped GUC variables with runtime
// info about this proxy instance. These are read by the gendb.info view via
// current_setting(). Must run before BuildSetupSQL on each connection.
func BuildInfoSQL(version, model, provider string, structuredOutput bool) string {
	escape := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
	so := "false"
	if structuredOutput {
		so = "true"
	}
	return fmt.Sprintf(
		"SET gendb.info.version = '%s';\n"+
			"SET gendb.info.model = '%s';\n"+
			"SET gendb.info.provider = '%s';\n"+
			"SET gendb.info.structured_output = '%s';\n",
		escape(version), escape(model), escape(provider), so,
	)
}

func buildDropProcedure(name string, def *CommandDef) string {
	var types []string
	for _, p := range def.Params {
		sqlType := p.SQLType
		if sqlType == "" {
			sqlType = "text"
		}
		types = append(types, sqlType)
	}
	return fmt.Sprintf("DROP PROCEDURE IF EXISTS gendb.%s(%s);\n", name, strings.Join(types, ", "))
}

func buildCreateProcedure(name string, def *CommandDef) string {
	var params []string
	for _, p := range def.Params {
		sqlType := p.SQLType
		if sqlType == "" {
			sqlType = "text"
		}
		param := fmt.Sprintf("    %s %s", p.Name, sqlType)
		if !p.Required && p.Default != "" {
			param += " DEFAULT " + p.Default
		}
		params = append(params, param)
	}

	paramList := ""
	if len(params) > 0 {
		paramList = "\n" + strings.Join(params, ",\n") + "\n"
	}

	return fmt.Sprintf(
		"CREATE OR REPLACE PROCEDURE gendb.%s(%s) LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'This procedure is handled by the GenDB proxy. Connect through the proxy on port 5433.'; END; $$;\n",
		name, paramList,
	)
}
