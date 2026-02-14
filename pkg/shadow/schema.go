package shadow

import "strings"

// SchemaName is the single schema where all generated tables live.
const SchemaName = "gendb"

// DefaultScenario is the scenario name used when none is specified.
const DefaultScenario = "default"

const separator = "__"

// ShadowTableName returns the shadow table name for a source table.
// Format: __scenario__schema__table
// Example: ShadowTableName("", "public", "users") returns "__default__public__users"
// Example: ShadowTableName("edge", "public", "users") returns "__edge__public__users"
func ShadowTableName(scenario, sourceSchema, tableName string) string {
	if scenario == "" {
		scenario = DefaultScenario
	}
	return separator + scenario + separator + sourceSchema + separator + tableName
}

// ParseShadowTableName splits a shadow table name back into scenario, source schema and table.
// Returns ("", "", "", false) if it doesn't match the convention.
func ParseShadowTableName(shadowName string) (scenario, sourceSchema, tableName string, ok bool) {
	if !strings.HasPrefix(shadowName, separator) {
		return "", "", "", false
	}
	rest := shadowName[len(separator):] // strip leading __
	// Split into exactly 3 parts: scenario, schema, table
	parts := strings.SplitN(rest, separator, 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
