package local

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/DanMarshall909/ai-router/internal/config"
	"github.com/stretchr/testify/require"
)

func TestModelIsProcessingReadsActiveSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, localModelSlotsPath, r.URL.Path, "because model activity is obtained from the slots endpoint")
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`[{"id":0,"is_processing":true}]`))
		require.NoError(t, err, "because the slot response should be written")
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err, "because the test server URL must be valid")
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err, "because the test server URL must include a port")
	mgr := NewManager(config.LocalModelConfig{Host: serverURL.Hostname(), Port: port}, nil)

	require.True(t, mgr.modelIsProcessing(), "because an active server slot must prevent idle shutdown")
}
