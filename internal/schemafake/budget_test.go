package schemafake

import (
	"context"
	"errors"
	"testing"

	"github.com/PandaPanY/apiyard/internal/genbudget"
)

func TestGenerationBudget(t *testing.T) {
	for _, schema := range []map[string]any{
		{"type": "array", "minItems": float64(10001)},
		{"type": "string", "minLength": float64(1000000000)},
		{"type": "string", "maxLength": float64(-1)},
		{"type": "array", "minItems": float64(200), "maxItems": float64(200), "items": map[string]any{"type": "array", "minItems": float64(200), "maxItems": float64(200)}},
	} {
		if _, err := Generate(schema, Options{}); !errors.Is(err, genbudget.ErrLimit) {
			t.Fatalf("schema %v: %v", schema, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GenerateContext(ctx, map[string]any{"type": "string"}, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
