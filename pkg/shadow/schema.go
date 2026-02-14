package shadow

// DeriveSchemaName returns the shadow schema name for a given source schema and key.
// For example, DeriveSchemaName("public", "autodb") returns "public_autodb".
func DeriveSchemaName(sourceSchema, key string) string {
	return sourceSchema + "_" + key
}
