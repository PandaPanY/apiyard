package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnfinishedMCPDisabledByDefault(t *testing.T) {
	for _, mode := range []string{"", "off"} {
		response := httptest.NewRecorder()
		New(nil, mode, "test").ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("mode %q: %d", mode, response.Code)
		}
	}
	response := httptest.NewRecorder()
	New(nil, "on", "test").ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code)
	}
}
