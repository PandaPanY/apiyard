package mockjs

import (
	"context"
	"errors"
	"testing"

	"github.com/PandaPanY/apiyard/internal/genbudget"
	"github.com/PandaPanY/apiyard/internal/jsjson"
)

func TestMockBudget(t *testing.T) {
	for _, v := range []any{
		jsjson.NewObject("list|1000000000", []any{1}),
		jsjson.NewObject("list|200", []any{jsjson.NewObject("list|200", []any{1})}),
		"@string(1000000000)", "@paragraph(1000000000)",
		RegExp{Source: "((a+)+){1000}"},
	} {
		if _, err := Mock(v); !errors.Is(err, genbudget.ErrLimit) {
			t.Fatalf("%v: %v", v, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MockContext(ctx, "@integer"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
