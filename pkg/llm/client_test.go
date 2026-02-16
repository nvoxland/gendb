package llm

import (
	"testing"
)

func TestFixJSON_Valid(t *testing.T) {
	input := `[{"id":1,"name":"Alice"}]`
	got := fixJSON(input)
	if got != input {
		t.Errorf("fixJSON changed valid JSON:\n got: %s\nwant: %s", got, input)
	}
}

func TestFixJSON_SingleQuotes(t *testing.T) {
	input := `[{'id': 1, 'name': 'Alice'}]`
	got := fixJSON(input)
	want := `[{"id": 1, "name": "Alice"}]`
	if got != want {
		t.Errorf("fixJSON single quotes:\n got: %s\nwant: %s", got, want)
	}
}

func TestFixJSON_TrailingComma(t *testing.T) {
	input := `[{"id":1},{"id":2},]`
	got := fixJSON(input)
	want := `[{"id":1},{"id":2}]`
	if got != want {
		t.Errorf("fixJSON trailing comma:\n got: %s\nwant: %s", got, want)
	}
}

func TestFixJSON_TrailingCommaInObject(t *testing.T) {
	input := `[{"id":1,"name":"Alice",}]`
	got := fixJSON(input)
	want := `[{"id":1,"name":"Alice"}]`
	if got != want {
		t.Errorf("fixJSON trailing comma in object:\n got: %s\nwant: %s", got, want)
	}
}

func TestFixJSON_DoubleComma(t *testing.T) {
	input := `[{"id":1},,{"id":2}]`
	got := fixJSON(input)
	want := `[{"id":1},{"id":2}]`
	if got != want {
		t.Errorf("fixJSON double comma:\n got: %s\nwant: %s", got, want)
	}
}

func TestFixJSON_PythonBooleans(t *testing.T) {
	input := `[{"active": True, "deleted": False, "value": None}]`
	got := fixJSON(input)
	want := `[{"active": true, "deleted": false, "value": null}]`
	if got != want {
		t.Errorf("fixJSON Python booleans:\n got: %s\nwant: %s", got, want)
	}
}

func TestFixJSON_TrailingCommaWithWhitespace(t *testing.T) {
	input := "[{\"id\":1} ,\n]"
	got := fixJSON(input)
	want := "[{\"id\":1} \n]"
	if got != want {
		t.Errorf("fixJSON trailing comma with whitespace:\n got: %q\nwant: %q", got, want)
	}
}

func TestRecoverStringifiedObjects(t *testing.T) {
	input := `["{\"id\":1,\"name\":\"Alice\"}", "{\"id\":2,\"name\":\"Bob\"}"]`
	rows := recoverStringifiedObjects(input)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("row 0 name = %v, want Alice", rows[0]["name"])
	}
	if rows[1]["name"] != "Bob" {
		t.Errorf("row 1 name = %v, want Bob", rows[1]["name"])
	}
}

func TestRecoverStringifiedObjects_Mixed(t *testing.T) {
	// Mix of real objects and stringified objects
	input := `[{"id":1,"name":"Alice"}, "{\"id\":2,\"name\":\"Bob\"}"]`
	rows := recoverStringifiedObjects(input)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestRecoverStringifiedObjects_SkipsGarbage(t *testing.T) {
	input := `[{"id":1}, "not json at all", "{\"id\":3}"]`
	rows := recoverStringifiedObjects(input)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestExtractJSONObjects(t *testing.T) {
	input := `some garbage {"id":1,"name":"Alice"} more stuff {"id":2,"name":"Bob"}`
	rows := extractJSONObjects(input)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Alice" {
		t.Errorf("row 0 name = %v, want Alice", rows[0]["name"])
	}
}

func TestExtractJSONObjects_NestedBraces(t *testing.T) {
	input := `[{"id":1,"meta":{"x":1}}, {"id":2}]`
	rows := extractJSONObjects(input)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	meta, ok := rows[0]["meta"].(map[string]any)
	if !ok {
		t.Fatal("row 0 meta is not a map")
	}
	if meta["x"] != float64(1) {
		t.Errorf("row 0 meta.x = %v, want 1", meta["x"])
	}
}

func TestParseResponse_ValidJSON(t *testing.T) {
	c := &Client{}
	rows, err := c.parseResponse(`[{"id":1},{"id":2}]`, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseResponse_StringifiedObjects(t *testing.T) {
	c := &Client{}
	input := `["{\"id\":1,\"name\":\"Alice\"}", "{\"id\":2,\"name\":\"Bob\"}"]`
	rows, err := c.parseResponse(input, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseResponse_TrailingCommas(t *testing.T) {
	c := &Client{}
	input := `[{"id":1,"name":"Alice",},{"id":2,"name":"Bob"},]`
	rows, err := c.parseResponse(input, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseResponse_BrokenArrayWithValidObjects(t *testing.T) {
	c := &Client{}
	input := `[{"id":1} garbage {"id":2}]`
	rows, err := c.parseResponse(input, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseResponse_DoubleCommas(t *testing.T) {
	c := &Client{}
	input := `[{"id":1},,{"id":2}]`
	rows, err := c.parseResponse(input, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseResponse_CodeBlock(t *testing.T) {
	c := &Client{}
	input := "```json\n[{\"id\":1}]\n```"
	rows, err := c.parseResponse(input, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
}

func TestParseResponse_TotalGarbage(t *testing.T) {
	c := &Client{}
	_, err := c.parseResponse("this is not json at all", false)
	if err == nil {
		t.Fatal("expected error for total garbage input")
	}
}
