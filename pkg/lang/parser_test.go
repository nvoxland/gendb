package lang

import (
	"testing"
)

func TestIsGenDBCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"CALL gendb.generate_data(table_name => 'users')", true},
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
	cmd, err := Parse("CALL gendb.generate_data(table_name => 'users', rows => 500)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Generate.Table)
	}
	if cmd.Generate.Rows != 500 {
		t.Errorf("got rows %d, want 500", cmd.Generate.Rows)
	}
	if cmd.Generate.Seed != nil {
		t.Errorf("expected nil seed, got %d", *cmd.Generate.Seed)
	}
}

func TestParseGenerateTableWithSeed(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(table_name => 'users', rows => 500, seed => 42)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Seed == nil || *cmd.Generate.Seed != 42 {
		t.Errorf("expected seed 42, got %v", cmd.Generate.Seed)
	}
}

func TestParseGenerateAll(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Table != "" {
		t.Errorf("expected empty table for all-tables generate, got %q", cmd.Generate.Table)
	}
}

func TestParseGenerateDataNoRows(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(table_name => 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Generate.Table)
	}
	if cmd.Generate.Rows != 0 {
		t.Errorf("expected rows 0 (default), got %d", cmd.Generate.Rows)
	}
}

func TestParseSemicolon(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(table_name => 'users');")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil || cmd.Generate.Table != "users" {
		t.Error("expected generate_data for users")
	}
}

func TestParseGenerateDataDoubleQuotedTableName(t *testing.T) {
	cmd, err := Parse(`CALL gendb.generate_data(table_name => "test1")`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Table != "test1" {
		t.Errorf("got table %q, want test1", cmd.Generate.Table)
	}
}

func TestParseGenerateDataDoubleQuotedWithRows(t *testing.T) {
	cmd, err := Parse(`CALL gendb.generate_data(table_name => "test1", rows => 100)`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Table != "test1" {
		t.Errorf("got table %q, want test1", cmd.Generate.Table)
	}
	if cmd.Generate.Rows != 100 {
		t.Errorf("got rows %d, want 100", cmd.Generate.Rows)
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
	if cmd.ReturnGenerated == nil {
		t.Fatal("expected ReturnGenerated command")
	}
	if cmd.ReturnGenerated.Table != "test1" {
		t.Errorf("got table %q, want test1", cmd.ReturnGenerated.Table)
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
	if cmd.ReturnGenerated == nil {
		t.Fatal("expected ReturnGenerated command")
	}
	if cmd.ReturnGenerated.Table != "users" {
		t.Errorf("got table %q, want users", cmd.ReturnGenerated.Table)
	}
}

func TestParseReturnActual(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_actual(table_name => 'test1')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.ReturnActual == nil {
		t.Fatal("expected ReturnActual command")
	}
	if cmd.ReturnActual.Table != "test1" {
		t.Errorf("got table %q, want test1", cmd.ReturnActual.Table)
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
	if cmd.ReturnActual == nil {
		t.Fatal("expected ReturnActual command")
	}
	if cmd.ReturnActual.Table != "orders" {
		t.Errorf("got table %q, want orders", cmd.ReturnActual.Table)
	}
}

func TestParseGenerateDataWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(table_name => 'users', rows => 100, scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Scenario != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.Generate.Scenario)
	}
}

func TestParseGenerateDataWithoutScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(table_name => 'users', rows => 100)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Scenario != "" {
		t.Errorf("got scenario %q, want empty", cmd.Generate.Scenario)
	}
}

func TestParseReturnGeneratedWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_generated(table_name => 'users', scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.ReturnGenerated == nil {
		t.Fatal("expected ReturnGenerated command")
	}
	if cmd.ReturnGenerated.Scenario != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.ReturnGenerated.Scenario)
	}
}

func TestParseReturnActualWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_actual(table_name => 'users', scenario => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.ReturnActual == nil {
		t.Fatal("expected ReturnActual command")
	}
	if cmd.ReturnActual.Scenario != "edge" {
		t.Errorf("got scenario %q, want edge", cmd.ReturnActual.Scenario)
	}
}

func TestParseRegenerateTable(t *testing.T) {
	cmd, err := Parse("CALL gendb.regenerate_data(table_name => 'users', rows => 200)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if cmd.Generate.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Generate.Table)
	}
	if cmd.Generate.Rows != 200 {
		t.Errorf("got rows %d, want 200", cmd.Generate.Rows)
	}
}
