package lang

import (
	"testing"
)

func TestIsAutoDBCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"AUTODB MODE SYNTHETIC", true},
		{"autodb mode synthetic", true},
		{"  AUTODB SHOW STATUS", true},
		{"SELECT * FROM users", false},
		{"AUTO", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsAutoDBCommand(tt.input)
		if got != tt.want {
			t.Errorf("IsAutoDBCommand(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseModeSynthetic(t *testing.T) {
	cmd, err := Parse("AUTODB MODE SYNTHETIC")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil {
		t.Fatal("expected Mode command")
	}
	if cmd.Mode.Mode != "SYNTHETIC" {
		t.Errorf("got mode %q, want SYNTHETIC", cmd.Mode.Mode)
	}
	if len(cmd.Mode.Tables) != 0 {
		t.Errorf("expected no tables, got %v", cmd.Mode.Tables)
	}
}

func TestParseModeReal(t *testing.T) {
	cmd, err := Parse("AUTODB MODE REAL")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil {
		t.Fatal("expected Mode command")
	}
	if cmd.Mode.Mode != "REAL" {
		t.Errorf("got mode %q, want REAL", cmd.Mode.Mode)
	}
}

func TestParseModeSyntheticForTable(t *testing.T) {
	cmd, err := Parse("AUTODB MODE SYNTHETIC FOR TABLE users, orders")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil {
		t.Fatal("expected Mode command")
	}
	if cmd.Mode.Mode != "SYNTHETIC" {
		t.Errorf("got mode %q, want SYNTHETIC", cmd.Mode.Mode)
	}
	if len(cmd.Mode.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(cmd.Mode.Tables))
	}
	if cmd.Mode.Tables[0] != "users" || cmd.Mode.Tables[1] != "orders" {
		t.Errorf("got tables %v, want [users, orders]", cmd.Mode.Tables)
	}
}

func TestParseGenerateTable(t *testing.T) {
	cmd, err := Parse("AUTODB GENERATE TABLE users ROWS 500")
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
	cmd, err := Parse("AUTODB GENERATE TABLE users ROWS 500 SEED 42")
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
	cmd, err := Parse("AUTODB GENERATE ALL ROWS 100")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Generate == nil {
		t.Fatal("expected Generate command")
	}
	if !cmd.Generate.All {
		t.Error("expected All=true")
	}
	if cmd.Generate.Rows != 100 {
		t.Errorf("got rows %d, want 100", cmd.Generate.Rows)
	}
}

func TestParseResetTable(t *testing.T) {
	cmd, err := Parse("AUTODB RESET TABLE users")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Reset == nil {
		t.Fatal("expected Reset command")
	}
	if cmd.Reset.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Reset.Table)
	}
}

func TestParseResetAll(t *testing.T) {
	cmd, err := Parse("AUTODB RESET ALL")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Reset == nil {
		t.Fatal("expected Reset command")
	}
	if !cmd.Reset.All {
		t.Error("expected All=true")
	}
}

func TestParseSetModelLocal(t *testing.T) {
	cmd, err := Parse("AUTODB SET MODEL LOCAL")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.Model == nil {
		t.Fatal("expected Set Model command")
	}
	if !cmd.Set.Model.Local {
		t.Error("expected Local=true")
	}
}

func TestParseSetModelWithKey(t *testing.T) {
	cmd, err := Parse("AUTODB SET MODEL 'openai/gpt-4o' KEY 'sk-abc123'")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.Model == nil {
		t.Fatal("expected Set Model command")
	}
	if cmd.Set.Model.Name != "openai/gpt-4o" {
		t.Errorf("got model %q, want openai/gpt-4o", cmd.Set.Model.Name)
	}
	if cmd.Set.Model.Key != "sk-abc123" {
		t.Errorf("got key %q, want sk-abc123", cmd.Set.Model.Key)
	}
}

func TestParseSetSeed(t *testing.T) {
	cmd, err := Parse("AUTODB SET SEED 42")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.Seed == nil {
		t.Fatal("expected Set Seed command")
	}
	if *cmd.Set.Seed != 42 {
		t.Errorf("got seed %d, want 42", *cmd.Set.Seed)
	}
}

func TestParseSetDefaultRows(t *testing.T) {
	cmd, err := Parse("AUTODB SET DEFAULT_ROWS 100")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.DefaultRows == nil {
		t.Fatal("expected Set DefaultRows command")
	}
	if *cmd.Set.DefaultRows != 100 {
		t.Errorf("got default_rows %d, want 100", *cmd.Set.DefaultRows)
	}
}

func TestParseSetTableColumnGenerator(t *testing.T) {
	cmd, err := Parse("AUTODB SET TABLE users COLUMN email GENERATOR 'internet.email'")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.TableCol == nil {
		t.Fatal("expected Set TableCol command")
	}
	if cmd.Set.TableCol.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Set.TableCol.Table)
	}
	if cmd.Set.TableCol.Column != "email" {
		t.Errorf("got column %q, want email", cmd.Set.TableCol.Column)
	}
	if cmd.Set.TableCol.Generator != "internet.email" {
		t.Errorf("got generator %q, want internet.email", cmd.Set.TableCol.Generator)
	}
}

func TestParseSetTableColumnWithPrompt(t *testing.T) {
	cmd, err := Parse("AUTODB SET TABLE users COLUMN bio GENERATOR 'llm' PROMPT 'Write a short bio'")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.TableCol == nil {
		t.Fatal("expected Set TableCol command")
	}
	if cmd.Set.TableCol.Generator != "llm" {
		t.Errorf("got generator %q, want llm", cmd.Set.TableCol.Generator)
	}
	if cmd.Set.TableCol.Prompt != "Write a short bio" {
		t.Errorf("got prompt %q, want 'Write a short bio'", cmd.Set.TableCol.Prompt)
	}
}

func TestParseSetTableColumnWithValues(t *testing.T) {
	cmd, err := Parse("AUTODB SET TABLE users COLUMN role GENERATOR 'one_of' VALUES ('admin', 'user', 'mod')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.TableCol == nil {
		t.Fatal("expected Set TableCol command")
	}
	if len(cmd.Set.TableCol.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(cmd.Set.TableCol.Values))
	}
	if cmd.Set.TableCol.Values[0] != "admin" {
		t.Errorf("got values[0] %q, want admin", cmd.Set.TableCol.Values[0])
	}
}

func TestParseShowStatus(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW STATUS")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil {
		t.Fatal("expected Show command")
	}
	if !cmd.Show.Status {
		t.Error("expected Status=true")
	}
}

func TestParseShowTables(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW TABLES")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || !cmd.Show.Tables {
		t.Fatal("expected Show Tables command")
	}
}

func TestParseShowTable(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW TABLE users")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || cmd.Show.TableInfo == nil {
		t.Fatal("expected Show TableInfo command")
	}
	if cmd.Show.TableInfo.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Show.TableInfo.Table)
	}
}

func TestParseShowConfig(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW CONFIG")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || !cmd.Show.Config {
		t.Fatal("expected Show Config command")
	}
}

func TestParseShowGenerationPlan(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW GENERATION PLAN")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || cmd.Show.GenPlan == nil {
		t.Fatal("expected Show GenPlan command")
	}
	if cmd.Show.GenPlan.Table != "" {
		t.Errorf("expected empty table, got %q", cmd.Show.GenPlan.Table)
	}
}

func TestParseShowGenerationPlanForTable(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW GENERATION PLAN FOR TABLE users")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || cmd.Show.GenPlan == nil {
		t.Fatal("expected Show GenPlan command")
	}
	if cmd.Show.GenPlan.Table != "users" {
		t.Errorf("got table %q, want users", cmd.Show.GenPlan.Table)
	}
}

func TestParseSyncSchema(t *testing.T) {
	cmd, err := Parse("AUTODB SYNC SCHEMA")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Sync == nil {
		t.Fatal("expected Sync command")
	}
	if !cmd.Sync.Schema {
		t.Error("expected Schema=true")
	}
}

func TestParseSemicolon(t *testing.T) {
	cmd, err := Parse("AUTODB MODE SYNTHETIC;")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil || cmd.Mode.Mode != "SYNTHETIC" {
		t.Error("expected SYNTHETIC mode")
	}
}

func TestParseCaseInsensitive(t *testing.T) {
	cmd, err := Parse("autodb mode synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil {
		t.Fatal("expected Mode command")
	}
}

func TestParseCreateProfile(t *testing.T) {
	cmd, err := Parse("AUTODB CREATE PROFILE 'load-test' (TABLE users ROWS 100000, TABLE orders ROWS 500000)")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.CreateProf == nil {
		t.Fatal("expected CreateProfile command")
	}
	if cmd.CreateProf.Name != "load-test" {
		t.Errorf("got name %q, want load-test", cmd.CreateProf.Name)
	}
	if len(cmd.CreateProf.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(cmd.CreateProf.Tables))
	}
	if cmd.CreateProf.Tables[0].Table != "users" || cmd.CreateProf.Tables[0].Rows != 100000 {
		t.Errorf("got table[0] = %v", cmd.CreateProf.Tables[0])
	}
}

func TestParseUseProfile(t *testing.T) {
	cmd, err := Parse("AUTODB USE PROFILE 'load-test'")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.UseProf == nil {
		t.Fatal("expected UseProfile command")
	}
	if cmd.UseProf.Name != "load-test" {
		t.Errorf("got name %q, want load-test", cmd.UseProf.Name)
	}
}

func TestParseDropProfile(t *testing.T) {
	cmd, err := Parse("AUTODB DROP PROFILE 'load-test'")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.DropProf == nil {
		t.Fatal("expected DropProfile command")
	}
	if cmd.DropProf.Name != "load-test" {
		t.Errorf("got name %q, want load-test", cmd.DropProf.Name)
	}
}

func TestParseShowProfiles(t *testing.T) {
	cmd, err := Parse("AUTODB SHOW PROFILES")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || !cmd.Show.Profiles {
		t.Fatal("expected Show Profiles command")
	}
}

func TestParseInvalidCommand(t *testing.T) {
	_, err := Parse("AUTODB INVALID COMMAND")
	if err == nil {
		t.Error("expected parse error for invalid command")
	}
}

func TestParseRegenerateTable(t *testing.T) {
	cmd, err := Parse("AUTODB REGENERATE TABLE users ROWS 200")
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
