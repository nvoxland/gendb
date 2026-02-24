package generator

import (
	"testing"

	"github.com/nvoxland/gendb/pkg/schema"
)

func TestCoerceValue_UnwrapsSingleElementSlice(t *testing.T) {
	col := &schema.Column{Name: "age", DataType: "integer"}
	got, err := coerceValue([]interface{}{float64(25)}, col)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := int64(25)
	if got != expected {
		t.Errorf("got %v (%T), want %v (%T)", got, got, expected, expected)
	}
}
