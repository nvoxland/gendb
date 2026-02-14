package lang

// Command is the top-level AST node for all AUTODB commands.
type Command struct {
	Mode            *ModeCommand
	Generate        *GenerateCommand
	Reset           *ResetCommand
	Set             *SetCommand
	Show            *ShowCommand
	Sync            *SyncCommand
	CreateProf      *CreateProfile
	UseProf         *UseProfile
	DropProf        *DropProfile
	ReturnGenerated *ReturnGeneratedCommand
	ReturnActual    *ReturnActualCommand
}

// ModeCommand: CALL autodb.mode(mode => 'synthetic', tables => 'users,orders')
type ModeCommand struct {
	Mode   string
	Tables []string
}

// GenerateCommand: CALL autodb.generate_data(table_name => 'users', rows => 500, seed => 42)
type GenerateCommand struct {
	Table string
	Rows  int
	Seed  *int64
}

// ResetCommand: CALL autodb.reset(table_name => 'users') or CALL autodb.reset()
type ResetCommand struct {
	Table string
}

// SetCommand covers various SET subcommands.
type SetCommand struct {
	Model       *SetModel
	Seed        *int64
	DefaultRows *int
	TableCol    *SetTableColumn
}

// SetModel: CALL autodb.set_model(name => 'gpt-4o', key => 'sk-x')
type SetModel struct {
	Name string
	Key  string
}

// SetTableColumn: CALL autodb.set_column(table_name => 't', column_name => 'c', generator => 'g', prompt => 'p', values => 'a,b')
type SetTableColumn struct {
	Table     string
	Column    string
	Generator string
	Prompt    string
	Values    []string
}

// ShowCommand: CALL autodb.show_status(), show_tables(), etc.
type ShowCommand struct {
	Status    bool
	Tables    bool
	Config    bool
	Profiles  bool
	GenPlan   *ShowPlan
	TableInfo *ShowTable
}

// ShowPlan: CALL autodb.show_generation_plan(table_name => 'users')
type ShowPlan struct {
	Table string
}

// ShowTable: CALL autodb.show_table(table_name => 'users')
type ShowTable struct {
	Table string
}

// SyncCommand: CALL autodb.sync_schema()
type SyncCommand struct {
	Schema bool
}

// CreateProfile: CALL autodb.create_profile(name => 'load-test', tables => 'users:100000,orders:500000')
type CreateProfile struct {
	Name   string
	Tables []ProfileTable
}

type ProfileTable struct {
	Table string
	Rows  int
}

// UseProfile: CALL autodb.use_profile(name => 'load-test')
type UseProfile struct {
	Name string
}

// DropProfile: CALL autodb.drop_profile(name => 'load-test')
type DropProfile struct {
	Name string
}

// ReturnGeneratedCommand: CALL autodb.return_generated(table_name => 'users')
type ReturnGeneratedCommand struct {
	Table string
}

// ReturnActualCommand: CALL autodb.return_actual(table_name => 'users')
type ReturnActualCommand struct {
	Table string
}
