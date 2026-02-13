package lang

// Command is the top-level AST node for all AUTODB commands.
type Command struct {
	Mode       *ModeCommand     `  "AUTODB" "MODE" @@`
	Generate   *GenerateCommand `| "AUTODB" ( "GENERATE" | "REGENERATE" ) @@`
	Reset      *ResetCommand    `| "AUTODB" "RESET" @@`
	Set        *SetCommand      `| "AUTODB" "SET" @@`
	Show       *ShowCommand     `| "AUTODB" "SHOW" @@`
	Sync       *SyncCommand     `| "AUTODB" "SYNC" @@`
	CreateProf *CreateProfile   `| "AUTODB" "CREATE" "PROFILE" @@`
	UseProf    *UseProfile      `| "AUTODB" "USE" "PROFILE" @@`
	DropProf   *DropProfile     `| "AUTODB" "DROP" "PROFILE" @@`
}

// ModeCommand: AUTODB MODE SYNTHETIC|REAL [FOR TABLE t1, t2, ...]
type ModeCommand struct {
	Mode   string   `@( "SYNTHETIC" | "REAL" )`
	Tables []string `( "FOR" "TABLE" @Ident ( "," @Ident )* )?`
}

// GenerateCommand: AUTODB GENERATE TABLE t ROWS n [SEED n] | AUTODB GENERATE ALL ROWS n
type GenerateCommand struct {
	All   bool   `(   @"ALL"`
	Table string `  | "TABLE" @Ident )`
	Rows  int    `( "ROWS" @Int )?`
	Seed  *int64 `( "SEED" @Int )?`
}

// ResetCommand: AUTODB RESET TABLE t | AUTODB RESET ALL
type ResetCommand struct {
	All   bool   `(   @"ALL"`
	Table string `  | "TABLE" @Ident )`
}

// SetCommand covers various SET subcommands.
type SetCommand struct {
	Model       *SetModel       `(   "MODEL" @@`
	Seed        *int64          `  | "SEED" @Int`
	DefaultRows *int            `  | "DEFAULT_ROWS" @Int`
	TableCol    *SetTableColumn `  | "TABLE" @@ )`
}

// SetModel: AUTODB SET MODEL 'name' [KEY 'key'] | AUTODB SET MODEL LOCAL
type SetModel struct {
	Local bool   `(   @"LOCAL"`
	Name  string `  | @String )`
	Key   string `( "KEY" @String )?`
}

// SetTableColumn: AUTODB SET TABLE t COLUMN c GENERATOR 'g' [PROMPT 'p'] [VALUES (...)]
type SetTableColumn struct {
	Table     string   `@Ident "COLUMN" `
	Column    string   `@Ident`
	Generator string   `"GENERATOR" @String`
	Prompt    string   `( "PROMPT" @String )?`
	Values    []string `( "VALUES" "(" @String ( "," @String )* ")" )?`
}

// ShowCommand: AUTODB SHOW STATUS|TABLES|CONFIG|PROFILES|...
type ShowCommand struct {
	Status    bool       `(   @"STATUS"`
	Tables    bool       `  | @"TABLES"`
	Config    bool       `  | @"CONFIG"`
	Profiles  bool       `  | @"PROFILES"`
	GenPlan   *ShowPlan  `  | "GENERATION" "PLAN" @@`
	TableInfo *ShowTable `  | "TABLE" @@ )`
}

// ShowPlan: [FOR TABLE t]
type ShowPlan struct {
	Table string `( "FOR" "TABLE" @Ident )?`
}

// ShowTable: AUTODB SHOW TABLE t
type ShowTable struct {
	Table string `@Ident`
}

// SyncCommand: AUTODB SYNC SCHEMA
type SyncCommand struct {
	Schema bool `@"SCHEMA"`
}

// CreateProfile: AUTODB CREATE PROFILE 'name' (TABLE t ROWS n, ...)
type CreateProfile struct {
	Name   string         `@String "("`
	Tables []ProfileTable `@@ ( "," @@ )* ")"`
}

type ProfileTable struct {
	Table string `"TABLE" @Ident`
	Rows  int    `"ROWS" @Int`
}

// UseProfile: AUTODB USE PROFILE 'name'
type UseProfile struct {
	Name string `@String`
}

// DropProfile: AUTODB DROP PROFILE 'name'
type DropProfile struct {
	Name string `@String`
}
