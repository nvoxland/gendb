package lang

// Command is the top-level AST node for all AUTODB commands.
type Command struct {
	Generate        *GenerateCommand
	ReturnGenerated *ReturnGeneratedCommand
	ReturnActual    *ReturnActualCommand
}

// GenerateCommand: CALL autodb.generate_data(table_name => 'users', rows => 500, seed => 42)
type GenerateCommand struct {
	Table string
	Rows  int
	Seed  *int64
}

// ReturnGeneratedCommand: CALL autodb.return_generated(table_name => 'users')
type ReturnGeneratedCommand struct {
	Table string
}

// ReturnActualCommand: CALL autodb.return_actual(table_name => 'users')
type ReturnActualCommand struct {
	Table string
}
