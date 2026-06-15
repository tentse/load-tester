package loadtest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const healthyGetServerMessage = "Hello from healthy mock GET server!"

func TestGetEndpoint(t *testing.T) {

	t.Run("Hitting healthy GET endpoint", func(t *testing.T) {

		mockGetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}

			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			w.Write([]byte(healthyGetServerMessage))
		}))

		defer mockGetServer.Close()

		statusCode, err := HitGetEndpoint(mockGetServer.URL)

		if err != nil {
			t.Fatalf("expected no error, but got: %v", err)
		}

		if statusCode != http.StatusOK {
			t.Errorf("got %v status code, want %v status code", http.StatusOK, statusCode)
		}

	})

}
