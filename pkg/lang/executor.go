package lang

import (
	"context"
	"fmt"
)

// Execute runs a parsed command by dispatching to its registered handler.
func Execute(ctx context.Context, cmd *Command) (*Result, error) {
	def, ok := Registry[cmd.Name]
	if !ok {
		return nil, fmt.Errorf("unrecognized GenDB command: %s", cmd.Name)
	}
	if def.Handler == nil {
		return nil, fmt.Errorf("%s not configured", cmd.Name)
	}
	return def.Handler(ctx, cmd.Args)
}
