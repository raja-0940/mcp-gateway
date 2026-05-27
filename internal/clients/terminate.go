package clients

import (
	"context"
	"fmt"
	"net/http"
)

const sessionIDHeader = "Mcp-Session-Id"

// TerminateSession sends an HTTP DELETE to the upstream MCP server to end the
// session identified by sessionID. This is used during gateway session cleanup
// for user-specific servers where the broker manages the session lifecycle
// rather than holding a long-lived mcp-go client.
func TerminateSession(ctx context.Context, serverURL, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, serverURL, nil)
	if err != nil {
		return fmt.Errorf("create DELETE request: %w", err)
	}
	req.Header.Set(sessionIDHeader, sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send DELETE request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}
