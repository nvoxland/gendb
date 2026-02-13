package lang

import (
	"context"
	"fmt"
	"strings"
)

// Result represents the output of an AUTODB command execution.
type Result struct {
	Tag     string     // command completion tag, e.g. "AUTODB MODE SYNTHETIC"
	Columns []string   // column names for tabular results
	Rows    [][]string // row data for tabular results
}

// State holds the runtime state for the AUTODB executor.
type State struct {
	GlobalMode string            // "real" or "synthetic"
	TableModes map[string]string // per-table mode overrides
}

// NewState creates a new executor state with defaults.
func NewState() *State {
	return &State{
		GlobalMode: "real",
		TableModes: make(map[string]string),
	}
}

// ModeForTable returns the effective mode for a given table.
func (s *State) ModeForTable(table string) string {
	if mode, ok := s.TableModes[table]; ok {
		return mode
	}
	return s.GlobalMode
}

// Executor runs parsed AUTODB commands.
type Executor struct {
	state *State
	// Callbacks for commands that need external resources.
	// These are set by the proxy or CLI.
	OnGenerate   func(ctx context.Context, table string, rows int, seed *int64) error
	OnReset      func(ctx context.Context, table string) error
	OnResetAll   func(ctx context.Context) error
	OnSyncSchema func(ctx context.Context) error
	OnSetModel   func(ctx context.Context, name, key string) error
	// Schema info callback for SHOW commands
	OnShowTables func(ctx context.Context) ([][]string, error) // returns rows of [name, row_count, mode]
	OnShowTable  func(ctx context.Context, table string) ([][]string, error)
}

// NewExecutor creates a new command executor.
func NewExecutor(state *State) *Executor {
	return &Executor{state: state}
}

// Execute runs a parsed command and returns the result.
func (e *Executor) Execute(ctx context.Context, cmd *Command) (*Result, error) {
	switch {
	case cmd.Mode != nil:
		return e.execMode(cmd.Mode)
	case cmd.Generate != nil:
		return e.execGenerate(ctx, cmd.Generate)
	case cmd.Reset != nil:
		return e.execReset(ctx, cmd.Reset)
	case cmd.Set != nil:
		return e.execSet(ctx, cmd.Set)
	case cmd.Show != nil:
		return e.execShow(ctx, cmd.Show)
	case cmd.Sync != nil:
		return e.execSync(ctx, cmd.Sync)
	default:
		return nil, fmt.Errorf("unrecognized AUTODB command")
	}
}

func (e *Executor) execMode(cmd *ModeCommand) (*Result, error) {
	mode := strings.ToLower(cmd.Mode)
	if len(cmd.Tables) > 0 {
		for _, t := range cmd.Tables {
			e.state.TableModes[t] = mode
		}
		return &Result{Tag: fmt.Sprintf("AUTODB MODE %s FOR %d TABLES", strings.ToUpper(mode), len(cmd.Tables))}, nil
	}
	e.state.GlobalMode = mode
	// Clear table overrides when setting global mode
	e.state.TableModes = make(map[string]string)
	return &Result{Tag: fmt.Sprintf("AUTODB MODE %s", strings.ToUpper(mode))}, nil
}

func (e *Executor) execGenerate(ctx context.Context, cmd *GenerateCommand) (*Result, error) {
	if e.OnGenerate == nil {
		return nil, fmt.Errorf("generate not configured")
	}

	table := cmd.Table
	if cmd.All {
		table = ""
	}

	if err := e.OnGenerate(ctx, table, cmd.Rows, cmd.Seed); err != nil {
		return nil, err
	}

	if cmd.All {
		return &Result{Tag: fmt.Sprintf("AUTODB GENERATE ALL ROWS %d", cmd.Rows)}, nil
	}
	return &Result{Tag: fmt.Sprintf("AUTODB GENERATE TABLE %s ROWS %d", cmd.Table, cmd.Rows)}, nil
}

func (e *Executor) execReset(ctx context.Context, cmd *ResetCommand) (*Result, error) {
	if cmd.All {
		if e.OnResetAll != nil {
			if err := e.OnResetAll(ctx); err != nil {
				return nil, err
			}
		}
		return &Result{Tag: "AUTODB RESET ALL"}, nil
	}
	if e.OnReset != nil {
		if err := e.OnReset(ctx, cmd.Table); err != nil {
			return nil, err
		}
	}
	return &Result{Tag: fmt.Sprintf("AUTODB RESET TABLE %s", cmd.Table)}, nil
}

func (e *Executor) execSet(ctx context.Context, cmd *SetCommand) (*Result, error) {
	switch {
	case cmd.Model != nil:
		if cmd.Model.Local {
			if e.OnSetModel != nil {
				if err := e.OnSetModel(ctx, "local", ""); err != nil {
					return nil, err
				}
			}
			return &Result{Tag: "AUTODB SET MODEL LOCAL"}, nil
		}
		if e.OnSetModel != nil {
			if err := e.OnSetModel(ctx, cmd.Model.Name, cmd.Model.Key); err != nil {
				return nil, err
			}
		}
		return &Result{Tag: fmt.Sprintf("AUTODB SET MODEL %s", cmd.Model.Name)}, nil
	case cmd.Seed != nil:
		return &Result{Tag: fmt.Sprintf("AUTODB SET SEED %d", *cmd.Seed)}, nil
	case cmd.DefaultRows != nil:
		return &Result{Tag: fmt.Sprintf("AUTODB SET DEFAULT_ROWS %d", *cmd.DefaultRows)}, nil
	case cmd.TableCol != nil:
		return &Result{Tag: fmt.Sprintf("AUTODB SET TABLE %s COLUMN %s", cmd.TableCol.Table, cmd.TableCol.Column)}, nil
	default:
		return nil, fmt.Errorf("unrecognized SET subcommand")
	}
}

func (e *Executor) execShow(ctx context.Context, cmd *ShowCommand) (*Result, error) {
	switch {
	case cmd.Status:
		return &Result{
			Tag:     "AUTODB SHOW STATUS",
			Columns: []string{"property", "value"},
			Rows: [][]string{
				{"mode", e.state.GlobalMode},
			},
		}, nil
	case cmd.Tables:
		if e.OnShowTables != nil {
			rows, err := e.OnShowTables(ctx)
			if err != nil {
				return nil, err
			}
			return &Result{
				Tag:     "AUTODB SHOW TABLES",
				Columns: []string{"table", "rows", "mode"},
				Rows:    rows,
			}, nil
		}
		return &Result{Tag: "AUTODB SHOW TABLES", Columns: []string{"table", "rows", "mode"}}, nil
	case cmd.Config:
		return &Result{Tag: "AUTODB SHOW CONFIG", Columns: []string{"key", "value"}}, nil
	case cmd.Profiles:
		return &Result{Tag: "AUTODB SHOW PROFILES", Columns: []string{"name"}}, nil
	case cmd.GenPlan != nil:
		return &Result{Tag: "AUTODB SHOW GENERATION PLAN", Columns: []string{"table", "column", "generator"}}, nil
	case cmd.TableInfo != nil:
		if e.OnShowTable != nil {
			rows, err := e.OnShowTable(ctx, cmd.TableInfo.Table)
			if err != nil {
				return nil, err
			}
			return &Result{
				Tag:     fmt.Sprintf("AUTODB SHOW TABLE %s", cmd.TableInfo.Table),
				Columns: []string{"column", "type", "nullable", "generator"},
				Rows:    rows,
			}, nil
		}
		return &Result{
			Tag:     fmt.Sprintf("AUTODB SHOW TABLE %s", cmd.TableInfo.Table),
			Columns: []string{"column", "type", "nullable", "generator"},
		}, nil
	default:
		return nil, fmt.Errorf("unrecognized SHOW subcommand")
	}
}

func (e *Executor) execSync(ctx context.Context, cmd *SyncCommand) (*Result, error) {
	if cmd.Schema && e.OnSyncSchema != nil {
		if err := e.OnSyncSchema(ctx); err != nil {
			return nil, err
		}
	}
	return &Result{Tag: "AUTODB SYNC SCHEMA"}, nil
}
