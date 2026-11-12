// Package mcp exposes the Model Context Protocol over stateless Streamable HTTP.
package mcp

import (
	"net/http"
	"strings"

	"github.com/PandaPanY/apiyard/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New creates the authenticated MCP endpoint. Tools are added in the following
// M7 steps; this endpoint first establishes transport and authentication.
func New(a *api.API, mode, version string) http.Handler {
	if mode == "off" || mode == "" {
		return http.NotFoundHandler()
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "ApiYard", Version: version}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			unauthorized(w)
			return
		}
		if _, err := a.AuthenticateMCP(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, prefix))); err != nil {
			unauthorized(w)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="ApiYard MCP"`)
	http.Error(w, "invalid or missing bearer token", http.StatusUnauthorized)
}
