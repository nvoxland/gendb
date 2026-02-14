package lang

// Command is the top-level AST node for all GENDB commands.
type Command struct {
	Generate        *GenerateCommand
	ReturnGenerated *ReturnGeneratedCommand
	ReturnActual    *ReturnActualCommand
}

// GenerateCommand: CALL gendb.generate_data(table_name => 'users', rows => 500, seed => 42, scenario => 'edge')
type GenerateCommand struct {
	Table    string
	Rows     int
	Seed     *int64
	Scenario string
}

// ReturnGeneratedCommand: CALL gendb.return_generated(table_name => 'users', scenario => 'edge')
type ReturnGeneratedCommand struct {
	Table    string
	Scenario string
}

// ReturnActualCommand: CALL gendb.return_actual(table_name => 'users', scenario => 'edge')
type ReturnActualCommand struct {
	Table    string
	Scenario string
}
