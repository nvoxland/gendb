package lang

import (
	"testing"
)

func TestIsGenDBCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"CALL gendb.generate_data(include_tables => 'users')", true},
		{"call gendb.return_generated(table_name => 'users')", true},
		{"  CALL gendb.return_actual(table_name => 'users')", true},
		{"SELECT * FROM users", false},
		{"CALL other.", false},
		{"CALL", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsGenDBCommand(tt.input)
		if got != tt.want {
			t.Errorf("IsGenDBCommand(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseGenerateTable(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'users', rows => 500)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include_tables"] != "users" {
		t.Errorf("got include_tables %q, want users", cmd.Args["include_tables"])
	}
	if cmd.Args["rows"] != "500" {
		t.Errorf("got rows %q, want 500", cmd.Args["rows"])
	}
	if _, ok := cmd.Args["seed"]; ok {
		t.Errorf("expected no seed arg, got %q", cmd.Args["seed"])
	}
}

func TestParseGenerateTableWithSeed(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'users', rows => 500, seed => 42)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["seed"] != "42" {
		t.Errorf("expected seed 42, got %q", cmd.Args["seed"])
	}
}

func TestParseGenerateAll(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include_tables"] != "" {
		t.Errorf("expected empty include_tables for all-tables generate, got %q", cmd.Args["include_tables"])
	}
}

func TestParseGenerateDataNoRows(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include_tables"] != "users" {
		t.Errorf("got include_tables %q, want users", cmd.Args["include_tables"])
	}
	if _, ok := cmd.Args["rows"]; ok {
		t.Errorf("expected no rows arg, got %q", cmd.Args["rows"])
	}
}

func TestParseSemicolon(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'users');")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" || cmd.Args["include_tables"] != "users" {
		t.Error("expected generate_data for users")
	}
}

func TestParseGenerateDataDoubleQuotedTableName(t *testing.T) {
	cmd, err := Parse(`CALL gendb.generate_data(include_tables => "test1")`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include_tables"] != "test1" {
		t.Errorf("got include_tables %q, want test1", cmd.Args["include_tables"])
	}
}

func TestParseGenerateDataDoubleQuotedWithRows(t *testing.T) {
	cmd, err := Parse(`CALL gendb.generate_data(include_tables => "test1", rows => 100)`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include_tables"] != "test1" {
		t.Errorf("got include_tables %q, want test1", cmd.Args["include_tables"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseInvalidCommand(t *testing.T) {
	_, err := Parse("CALL gendb.nonexistent()")
	if err == nil {
		t.Error("expected parse error for invalid command")
	}
}

func TestParseReturnGenerated(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_generated(table_name => 'test1')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "test1" {
		t.Errorf("got table_name %q, want test1", cmd.Args["table_name"])
	}
}

func TestParseReturnGeneratedMissingTable(t *testing.T) {
	_, err := Parse("CALL gendb.return_generated()")
	if err == nil {
		t.Error("expected parse error for missing table_name")
	}
}

func TestParseReturnGeneratedCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call gendb.Return_Generated(table_name => 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "users" {
		t.Errorf("got table_name %q, want users", cmd.Args["table_name"])
	}
}

func TestParseReturnActual(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_actual(table_name => 'test1')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_actual" {
		t.Fatalf("expected return_actual, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "test1" {
		t.Errorf("got table_name %q, want test1", cmd.Args["table_name"])
	}
}

func TestParseReturnActualMissingTable(t *testing.T) {
	_, err := Parse("CALL gendb.return_actual()")
	if err == nil {
		t.Error("expected parse error for missing table_name")
	}
}

func TestParseReturnActualCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call gendb.Return_Actual(table_name => 'orders')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_actual" {
		t.Fatalf("expected return_actual, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "orders" {
		t.Errorf("got table_name %q, want orders", cmd.Args["table_name"])
	}
}

func TestParseGenerateDataWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'users', rows => 100, scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["scenario"] != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.Args["scenario"])
	}
}

func TestParseGenerateDataWithoutScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'users', rows => 100)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if _, ok := cmd.Args["scenario"]; ok {
		t.Errorf("expected no scenario arg, got %q", cmd.Args["scenario"])
	}
}

func TestParseReturnGeneratedWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_generated(table_name => 'users', scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["scenario"] != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.Args["scenario"])
	}
}

func TestParseReturnActualWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_actual(table_name => 'users', scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_actual" {
		t.Fatalf("expected return_actual, got %q", cmd.Name)
	}
	if cmd.Args["scenario"] != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.Args["scenario"])
	}
}

func TestParseSyncNoArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.sync()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "sync" {
		t.Fatalf("expected sync, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "" {
		t.Errorf("expected empty table_name, got %q", cmd.Args["table_name"])
	}
	if cmd.Args["scenario"] != "" {
		t.Errorf("expected empty scenario, got %q", cmd.Args["scenario"])
	}
}

func TestParseSyncWithTable(t *testing.T) {
	cmd, err := Parse("CALL gendb.sync(table_name => 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "sync" {
		t.Fatalf("expected sync, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "users" {
		t.Errorf("got table_name %q, want users", cmd.Args["table_name"])
	}
	if _, ok := cmd.Args["scenario"]; ok {
		t.Errorf("expected no scenario arg, got %q", cmd.Args["scenario"])
	}
}

func TestParseSyncWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.sync(scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "sync" {
		t.Fatalf("expected sync, got %q", cmd.Name)
	}
	if _, ok := cmd.Args["table_name"]; ok {
		t.Errorf("expected no table_name arg, got %q", cmd.Args["table_name"])
	}
	if cmd.Args["scenario"] != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.Args["scenario"])
	}
}

func TestParseSyncWithBoth(t *testing.T) {
	cmd, err := Parse("CALL gendb.sync(table_name => 'users', scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "sync" {
		t.Fatalf("expected sync, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "users" {
		t.Errorf("got table_name %q, want users", cmd.Args["table_name"])
	}
	if cmd.Args["scenario"] != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.Args["scenario"])
	}
}

func TestParseUnknownParameter(t *testing.T) {
	_, err := Parse("CALL gendb.generate_data(include_tables => 'users', bogus => 'value')")
	if err == nil {
		t.Error("expected parse error for unknown parameter")
	}
}

func TestParsePositionalArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data('users', NULL, 100)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args["include_tables"] != "users" {
		t.Errorf("got include_tables %q, want users", cmd.Args["include_tables"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseMixedArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data('users', exclude_tables => 'temp*', rows => 100)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args["include_tables"] != "users" {
		t.Errorf("got include_tables %q, want users", cmd.Args["include_tables"])
	}
	if cmd.Args["exclude_tables"] != "temp*" {
		t.Errorf("got exclude_tables %q, want temp*", cmd.Args["exclude_tables"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParsePositionalWithNull(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(NULL, NULL, 100)")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cmd.Args["include_tables"]; ok {
		t.Errorf("expected no include_tables for NULL, got %q", cmd.Args["include_tables"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseColonEqualsNotation(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables := 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args["include_tables"] != "users" {
		t.Errorf("got include_tables %q, want users", cmd.Args["include_tables"])
	}
}

func TestParseNamedThenPositionalError(t *testing.T) {
	_, err := Parse("CALL gendb.generate_data(include_tables => 'users', 100)")
	if err == nil {
		t.Error("expected error for positional arg after named arg")
	}
}

func TestParseTooManyPositionalError(t *testing.T) {
	_, err := Parse("CALL gendb.generate_data('users', NULL, 100, 42, 'default', true, 'extra')")
	if err == nil {
		t.Error("expected error for too many positional args")
	}
}

func TestParsePositionalReturnGenerated(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_generated('users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["table_name"] != "users" {
		t.Errorf("got table_name %q, want users", cmd.Args["table_name"])
	}
}

func TestParseNamedNullOmitted(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => NULL, rows => 100)")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cmd.Args["include_tables"]; ok {
		t.Errorf("expected no include_tables for NULL, got %q", cmd.Args["include_tables"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseGenerateDataWithExcludeTables(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(exclude_tables => 'temp*')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["exclude_tables"] != "temp*" {
		t.Errorf("got exclude_tables %q, want temp*", cmd.Args["exclude_tables"])
	}
}

func TestParseGenerateDataWithIncludeAndExclude(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include_tables => 'user*', exclude_tables => 'user_logs')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include_tables"] != "user*" {
		t.Errorf("got include_tables %q, want user*", cmd.Args["include_tables"])
	}
	if cmd.Args["exclude_tables"] != "user_logs" {
		t.Errorf("got exclude_tables %q, want user_logs", cmd.Args["exclude_tables"])
	}
}
