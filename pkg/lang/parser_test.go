package lang

import (
	"testing"
)

func TestIsAutoDBCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"CALL autodb.mode(mode => 'synthetic')", true},
		{"call autodb.mode(mode => 'synthetic')", true},
		{"  CALL autodb.show_status()", true},
		{"SELECT * FROM users", false},
		{"CALL other.", false},
		{"CALL", false},
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
	cmd, err := Parse("CALL autodb.mode(mode => 'synthetic')")
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
	cmd, err := Parse("CALL autodb.mode(mode => 'real')")
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
	cmd, err := Parse("CALL autodb.mode(mode => 'synthetic', tables => 'users,orders')")
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
	cmd, err := Parse("CALL autodb.generate_data(table_name => 'users', rows => 500)")
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
	cmd, err := Parse("CALL autodb.generate_data(table_name => 'users', rows => 500, seed => 42)")
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
	cmd, err := Parse("CALL autodb.generate_data()")
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
	cmd, err := Parse("CALL autodb.generate_data(table_name => 'users')")
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

func TestParseResetTable(t *testing.T) {
	cmd, err := Parse("CALL autodb.reset(table_name => 'users')")
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
	cmd, err := Parse("CALL autodb.reset()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Reset == nil {
		t.Fatal("expected Reset command")
	}
	if cmd.Reset.Table != "" {
		t.Errorf("expected empty table for reset all, got %q", cmd.Reset.Table)
	}
}

func TestParseSetModelLocal(t *testing.T) {
	cmd, err := Parse("CALL autodb.set_model(name => 'local')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Set == nil || cmd.Set.Model == nil {
		t.Fatal("expected Set Model command")
	}
	if cmd.Set.Model.Name != "local" {
		t.Errorf("got name %q, want local", cmd.Set.Model.Name)
	}
}

func TestParseSetModelWithKey(t *testing.T) {
	cmd, err := Parse("CALL autodb.set_model(name => 'openai/gpt-4o', key => 'sk-abc123')")
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
	cmd, err := Parse("CALL autodb.set_seed(value => 42)")
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
	cmd, err := Parse("CALL autodb.set_default_rows(value => 100)")
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
	cmd, err := Parse("CALL autodb.set_column(table_name => 'users', column_name => 'email', generator => 'internet.email')")
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
	cmd, err := Parse("CALL autodb.set_column(table_name => 'users', column_name => 'bio', generator => 'llm', prompt => 'Write a short bio')")
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
	cmd, err := Parse("CALL autodb.set_column(table_name => 'users', column_name => 'role', generator => 'one_of', values => 'admin,user,mod')")
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
	cmd, err := Parse("CALL autodb.show_status()")
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
	cmd, err := Parse("CALL autodb.show_tables()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || !cmd.Show.Tables {
		t.Fatal("expected Show Tables command")
	}
}

func TestParseShowTable(t *testing.T) {
	cmd, err := Parse("CALL autodb.show_table(table_name => 'users')")
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
	cmd, err := Parse("CALL autodb.show_config()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || !cmd.Show.Config {
		t.Fatal("expected Show Config command")
	}
}

func TestParseShowGenerationPlan(t *testing.T) {
	cmd, err := Parse("CALL autodb.show_generation_plan()")
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
	cmd, err := Parse("CALL autodb.show_generation_plan(table_name => 'users')")
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
	cmd, err := Parse("CALL autodb.sync_schema()")
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
	cmd, err := Parse("CALL autodb.mode(mode => 'synthetic');")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil || cmd.Mode.Mode != "SYNTHETIC" {
		t.Error("expected SYNTHETIC mode")
	}
}

func TestParseCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call Autodb.Mode(mode => 'synthetic')")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode == nil {
		t.Fatal("expected Mode command")
	}
}

func TestParseCreateProfile(t *testing.T) {
	cmd, err := Parse("CALL autodb.create_profile(name => 'load-test', tables => 'users:100000,orders:500000')")
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
	cmd, err := Parse("CALL autodb.use_profile(name => 'load-test')")
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
	cmd, err := Parse("CALL autodb.drop_profile(name => 'load-test')")
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
	cmd, err := Parse("CALL autodb.show_profiles()")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Show == nil || !cmd.Show.Profiles {
		t.Fatal("expected Show Profiles command")
	}
}

func TestParseGenerateDataDoubleQuotedTableName(t *testing.T) {
	cmd, err := Parse(`CALL autodb.generate_data(table_name => "test1")`)
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
	cmd, err := Parse(`CALL autodb.generate_data(table_name => "test1", rows => 100)`)
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
	_, err := Parse("CALL autodb.nonexistent()")
	if err == nil {
		t.Error("expected parse error for invalid command")
	}
}

func TestParseReturnGenerated(t *testing.T) {
	cmd, err := Parse("CALL autodb.return_generated(table_name => 'test1')")
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
	_, err := Parse("CALL autodb.return_generated()")
	if err == nil {
		t.Error("expected parse error for missing table_name")
	}
}

func TestParseReturnGeneratedCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call autodb.Return_Generated(table_name => 'users')")
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
	cmd, err := Parse("CALL autodb.return_actual(table_name => 'test1')")
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
	_, err := Parse("CALL autodb.return_actual()")
	if err == nil {
		t.Error("expected parse error for missing table_name")
	}
}

func TestParseReturnActualCaseInsensitive(t *testing.T) {
	cmd, err := Parse("call autodb.Return_Actual(table_name => 'orders')")
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

func TestParseRegenerateTable(t *testing.T) {
	cmd, err := Parse("CALL autodb.regenerate_data(table_name => 'users', rows => 200)")
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
