package lang

import (
	"context"
	"fmt"
)

// Result represents the output of an AUTODB command execution.
type Result struct {
	Tag     string     // command completion tag, e.g. "AUTODB GENERATE DATA FOR users ROWS 500"
	Columns []string   // column names for tabular results
	Rows    [][]string // row data for tabular results
}

// Executor runs parsed AUTODB commands.
type Executor struct {
	OnGenerate        func(ctx context.Context, table string, rows int, seed *int64) error
	OnReturnGenerated func(ctx context.Context, table string) error
	OnReturnActual    func(ctx context.Context, table string) error
}

// NewExecutor creates a new command executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// Execute runs a parsed command and returns the result.
func (e *Executor) Execute(ctx context.Context, cmd *Command) (*Result, error) {
	switch {
	case cmd.Generate != nil:
		return e.execGenerate(ctx, cmd.Generate)
	case cmd.ReturnGenerated != nil:
		return e.execReturnGenerated(ctx, cmd.ReturnGenerated)
	case cmd.ReturnActual != nil:
		return e.execReturnActual(ctx, cmd.ReturnActual)
	default:
		return nil, fmt.Errorf("unrecognized AUTODB command")
	}
}

func (e *Executor) execGenerate(ctx context.Context, cmd *GenerateCommand) (*Result, error) {
	if e.OnGenerate == nil {
		return nil, fmt.Errorf("generate not configured")
	}

	if err := e.OnGenerate(ctx, cmd.Table, cmd.Rows, cmd.Seed); err != nil {
		return nil, err
	}

	if cmd.Table == "" {
		return &Result{Tag: fmt.Sprintf("AUTODB GENERATE DATA ROWS %d", cmd.Rows)}, nil
	}
	return &Result{Tag: fmt.Sprintf("AUTODB GENERATE DATA FOR %s ROWS %d", cmd.Table, cmd.Rows)}, nil
}

func (e *Executor) execReturnGenerated(ctx context.Context, cmd *ReturnGeneratedCommand) (*Result, error) {
	if e.OnReturnGenerated == nil {
		return nil, fmt.Errorf("return_generated not configured")
	}
	if err := e.OnReturnGenerated(ctx, cmd.Table); err != nil {
		return nil, err
	}
	return &Result{Tag: fmt.Sprintf("AUTODB RETURN GENERATED %s", cmd.Table)}, nil
}

func (e *Executor) execReturnActual(ctx context.Context, cmd *ReturnActualCommand) (*Result, error) {
	if e.OnReturnActual == nil {
		return nil, fmt.Errorf("return_actual not configured")
	}
	if err := e.OnReturnActual(ctx, cmd.Table); err != nil {
		return nil, err
	}
	return &Result{Tag: fmt.Sprintf("AUTODB RETURN ACTUAL %s", cmd.Table)}, nil
}
