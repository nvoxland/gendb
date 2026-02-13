package lang

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var autodbLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Keyword", Pattern: `(?i)\b(AUTODB|MODE|SYNTHETIC|REAL|FOR|TABLE|GENERATE|REGENERATE|RESET|ALL|ROWS|SEED|SET|MODEL|LOCAL|KEY|DEFAULT_ROWS|COLUMN|GENERATOR|PROMPT|VALUES|SHOW|STATUS|TABLES|CONFIG|PROFILES|GENERATION|PLAN|SYNC|SCHEMA|CREATE|PROFILE|USE|DROP)\b`},
	{Name: "String", Pattern: `'[^']*'`},
	{Name: "Int", Pattern: `\d+`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "Punct", Pattern: `[(),;]`},
	{Name: "Whitespace", Pattern: `\s+`},
})

var parser = participle.MustBuild[Command](
	participle.Lexer(autodbLexer),
	participle.CaseInsensitive("Keyword"),
	participle.Unquote("String"),
	participle.Elide("Whitespace"),
)

// Parse parses an AUTODB command string into an AST.
func Parse(input string) (*Command, error) {
	// Strip trailing semicolons
	input = strings.TrimSpace(input)
	input = strings.TrimSuffix(input, ";")
	input = strings.TrimSpace(input)

	cmd, err := parser.ParseString("", input)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return cmd, nil
}

// IsAutoDBCommand checks if a SQL string starts with AUTODB (case-insensitive).
func IsAutoDBCommand(sql string) bool {
	sql = strings.TrimSpace(sql)
	if len(sql) < 6 {
		return false
	}
	return strings.EqualFold(sql[:6], "AUTODB")
}
