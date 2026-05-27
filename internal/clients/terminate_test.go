package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerminateSession(t *testing.T) {
	t.Run("sends DELETE with session header", func(t *testing.T) {
		var gotMethod string
		var gotSessionID string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotSessionID = r.Header.Get(sessionIDHeader)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := TerminateSession(context.Background(), srv.URL, "test-session-123")
		require.NoError(t, err)
		assert.Equal(t, http.MethodDelete, gotMethod)
		assert.Equal(t, "test-session-123", gotSessionID)
	})

	t.Run("handles server error gracefully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		err := TerminateSession(context.Background(), srv.URL, "session-456")
		assert.NoError(t, err)
	})

	t.Run("returns error on connection failure", func(t *testing.T) {
		err := TerminateSession(context.Background(), "http://127.0.0.1:1", "session-789")
		assert.Error(t, err)
	})
}
