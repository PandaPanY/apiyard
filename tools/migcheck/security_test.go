package main

import (
	"encoding/json"
	"testing"

	"github.com/PandaPanY/apiyard/internal/compat"
)

func TestSecurityDifferencesKeepOriginalToken(t *testing.T) {
	original := map[string]any{"errcode": json.Number("0"), "data": "aabb"}
	want := compat.Exchange{Response: original}
	got := compat.Exchange{Response: map[string]any{"errcode": json.Number("0"), "data": "ccdd"}}
	if err := securityDifferences("/api/project/token?project_id=1", &want, &got); err != nil {
		t.Fatal(err)
	}
	if original["data"] != "aabb" {
		t.Fatal("original token changed")
	}
	diff, err := compat.Diff([]compat.Exchange{want}, []compat.Exchange{got})
	if err != nil || len(diff) != 0 {
		t.Fatalf("%v %v", diff, err)
	}
	got.Response = map[string]any{"data": nil}
	if err := securityDifferences("/api/project/token?project_id=1", &want, &got); err == nil {
		t.Fatal("failed issuance must not be normalized")
	}
}

func TestLegacyRoleDifferenceDoesNotMaskProjectData(t *testing.T) {
	want := compat.Exchange{Response: map[string]any{"data": map[string]any{"role": "admin", "name": "private"}}}
	got := compat.Exchange{Response: map[string]any{"data": map[string]any{"role": false, "name": "wrong"}}}
	if err := securityDifferences("/api/project/get?token=old", &want, &got); err != nil {
		t.Fatal(err)
	}
	diff, err := compat.Diff([]compat.Exchange{want}, []compat.Exchange{got})
	if err != nil || len(diff) == 0 {
		t.Fatalf("must still catch project difference: %v %v", diff, err)
	}
	got.Response = map[string]any{"data": map[string]any{"role": "admin"}}
	if err := securityDifferences("/api/project/get?token=old", &want, &got); err == nil {
		t.Fatal("legacy admin role must fail")
	}
}
