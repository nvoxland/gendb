package lang

import (
	"strings"
	"testing"
)

func TestIsGenDBCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"CALL gendb.generate_data(include => 'users')", true},
		{"call gendb.return_generated(include => 'users')", true},
		{"  CALL gendb.return_actual(include => 'users')", true},
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
	cmd, err := Parse("CALL gendb.generate_data(include => 'users', rows => 500)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
	if cmd.Args["rows"] != "500" {
		t.Errorf("got rows %q, want 500", cmd.Args["rows"])
	}
	if _, ok := cmd.Args["seed"]; ok {
		t.Errorf("expected no seed arg, got %q", cmd.Args["seed"])
	}
}

func TestParseGenerateTableWithSeed(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include => 'users', rows => 500, seed => 42)")
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
	if cmd.Args["include"] != "" {
		t.Errorf("expected empty include for all-tables generate, got %q", cmd.Args["include"])
	}
}

func TestParseGenerateDataNoRows(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include => 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
	if _, ok := cmd.Args["rows"]; ok {
		t.Errorf("expected no rows arg, got %q", cmd.Args["rows"])
	}
}

func TestParseSemicolon(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include => 'users');")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" || cmd.Args["include"] != "users" {
		t.Error("expected generate_data for users")
	}
}

func TestParseGenerateDataDoubleQuotedTableName(t *testing.T) {
	cmd, err := Parse(`CALL gendb.generate_data(include => "test1")`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "test1" {
		t.Errorf("got include %q, want test1", cmd.Args["include"])
	}
}

func TestParseGenerateDataDoubleQuotedWithRows(t *testing.T) {
	cmd, err := Parse(`CALL gendb.generate_data(include => "test1", rows => 100)`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "test1" {
		t.Errorf("got include %q, want test1", cmd.Args["include"])
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
	cmd, err := Parse("CALL gendb.return_generated(include => 'test1')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "test1" {
		t.Errorf("got include %q, want test1", cmd.Args["include"])
	}
}

func TestParseReturnGeneratedNoArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_generated()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "" {
		t.Errorf("expected empty include, got %q", cmd.Args["include"])
	}
}

func TestParseReturnGeneratedCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call gendb.Return_Generated(include => 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_generated" {
		t.Fatalf("expected return_generated, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
}

func TestParseReturnActual(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_actual(include => 'test1')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_actual" {
		t.Fatalf("expected return_actual, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "test1" {
		t.Errorf("got include %q, want test1", cmd.Args["include"])
	}
}

func TestParseReturnActualNoArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.return_actual()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_actual" {
		t.Fatalf("expected return_actual, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "" {
		t.Errorf("expected empty include, got %q", cmd.Args["include"])
	}
}

func TestParseReturnActualCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call gendb.Return_Actual(include => 'orders')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "return_actual" {
		t.Fatalf("expected return_actual, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "orders" {
		t.Errorf("got include %q, want orders", cmd.Args["include"])
	}
}

func TestParseGenerateDataWithScenario(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include => 'users', rows => 100, scenario => 'edge')")
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
	cmd, err := Parse("CALL gendb.generate_data(include => 'users', rows => 100)")
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
	cmd, err := Parse("CALL gendb.return_generated(include => 'users', scenario => 'edge')")
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
	cmd, err := Parse("CALL gendb.return_actual(include => 'users', scenario => 'edge')")
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
	_, err := Parse("CALL gendb.generate_data(include => 'users', bogus => 'value')")
	if err == nil {
		t.Error("expected parse error for unknown parameter")
	}
}

func TestParsePositionalArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data('users', NULL, 100)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseMixedArgs(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data('users', exclude => 'temp*', rows => 100)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
	if cmd.Args["exclude"] != "temp*" {
		t.Errorf("got exclude %q, want temp*", cmd.Args["exclude"])
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
	if _, ok := cmd.Args["include"]; ok {
		t.Errorf("expected no include for NULL, got %q", cmd.Args["include"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseColonEqualsNotation(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include := 'users')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
}

func TestParseNamedThenPositionalError(t *testing.T) {
	_, err := Parse("CALL gendb.generate_data(include => 'users', 100)")
	if err == nil {
		t.Error("expected error for positional arg after named arg")
	}
}

func TestParseTooManyPositionalError(t *testing.T) {
	_, err := Parse("CALL gendb.generate_data('users', NULL, 100, 42, 'default', true, 'hint', 'extra')")
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
	if cmd.Args["include"] != "users" {
		t.Errorf("got include %q, want users", cmd.Args["include"])
	}
}

func TestParseNamedNullOmitted(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include => NULL, rows => 100)")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cmd.Args["include"]; ok {
		t.Errorf("expected no include for NULL, got %q", cmd.Args["include"])
	}
	if cmd.Args["rows"] != "100" {
		t.Errorf("got rows %q, want 100", cmd.Args["rows"])
	}
}

func TestParseGenerateDataWithExcludeTables(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(exclude => 'temp*')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["exclude"] != "temp*" {
		t.Errorf("got exclude %q, want temp*", cmd.Args["exclude"])
	}
}

func TestParseGenerateDataWithIncludeAndExclude(t *testing.T) {
	cmd, err := Parse("CALL gendb.generate_data(include => 'user*', exclude => 'user_logs')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "generate_data" {
		t.Fatalf("expected generate_data, got %q", cmd.Name)
	}
	if cmd.Args["include"] != "user*" {
		t.Errorf("got include %q, want user*", cmd.Args["include"])
	}
	if cmd.Args["exclude"] != "user_logs" {
		t.Errorf("got exclude %q, want user_logs", cmd.Args["exclude"])
	}
}

func TestParseCreateScenarioNamed(t *testing.T) {
	cmd, err := Parse("CALL gendb.create_scenario(name => 'edge', prompt => 'Use extreme values')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "create_scenario" {
		t.Fatalf("expected create_scenario, got %q", cmd.Name)
	}
	if cmd.Args["name"] != "edge" {
		t.Errorf("got name %q, want edge", cmd.Args["name"])
	}
	if cmd.Args["prompt"] != "Use extreme values" {
		t.Errorf("got prompt %q, want 'Use extreme values'", cmd.Args["prompt"])
	}
}

func TestParseCreateScenarioPositional(t *testing.T) {
	cmd, err := Parse("CALL gendb.create_scenario('edge', 'Use extreme values')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "create_scenario" {
		t.Fatalf("expected create_scenario, got %q", cmd.Name)
	}
	if cmd.Args["name"] != "edge" {
		t.Errorf("got name %q, want edge", cmd.Args["name"])
	}
	if cmd.Args["prompt"] != "Use extreme values" {
		t.Errorf("got prompt %q, want 'Use extreme values'", cmd.Args["prompt"])
	}
}

func TestParseCreateScenarioNameOnly(t *testing.T) {
	cmd, err := Parse("CALL gendb.create_scenario(name => 'edge')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "create_scenario" {
		t.Fatalf("expected create_scenario, got %q", cmd.Name)
	}
	if cmd.Args["name"] != "edge" {
		t.Errorf("got name %q, want edge", cmd.Args["name"])
	}
	if _, ok := cmd.Args["prompt"]; ok {
		t.Errorf("expected no prompt arg, got %q", cmd.Args["prompt"])
	}
}

func TestParseUpdateScenarioNamed(t *testing.T) {
	cmd, err := Parse("CALL gendb.update_scenario(name => 'edge', prompt => 'Updated prompt')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "update_scenario" {
		t.Fatalf("expected update_scenario, got %q", cmd.Name)
	}
	if cmd.Args["name"] != "edge" {
		t.Errorf("got name %q, want edge", cmd.Args["name"])
	}
	if cmd.Args["prompt"] != "Updated prompt" {
		t.Errorf("got prompt %q, want 'Updated prompt'", cmd.Args["prompt"])
	}
}

func TestParseUpdateScenarioPositional(t *testing.T) {
	cmd, err := Parse("CALL gendb.update_scenario('edge', 'Updated prompt')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "update_scenario" {
		t.Fatalf("expected update_scenario, got %q", cmd.Name)
	}
	if cmd.Args["name"] != "edge" {
		t.Errorf("got name %q, want edge", cmd.Args["name"])
	}
	if cmd.Args["prompt"] != "Updated prompt" {
		t.Errorf("got prompt %q, want 'Updated prompt'", cmd.Args["prompt"])
	}
}

func TestBuildSetupSQL_ScenarioTableSchema(t *testing.T) {
	sql := BuildSetupSQL()

	// Scenario table should have an autoincrement PK and name as varchar(255) unique.
	if !strings.Contains(sql, "id BIGSERIAL PRIMARY KEY") {
		t.Error("scenario table should have id BIGSERIAL PRIMARY KEY")
	}
	if !strings.Contains(sql, "name VARCHAR(255) NOT NULL UNIQUE") {
		t.Error("scenario table should have name VARCHAR(255) NOT NULL UNIQUE")
	}
	// History table FK should reference scenario(id) not scenario(name).
	if strings.Contains(sql, "REFERENCES gendb.scenario(name)") {
		t.Error("history table should not reference scenario(name)")
	}
	if !strings.Contains(sql, "scenario_id BIGINT REFERENCES gendb.scenario(id)") {
		t.Error("history table should have scenario_id BIGINT REFERENCES gendb.scenario(id)")
	}
}
