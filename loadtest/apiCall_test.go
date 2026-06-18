package loadtest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetEndpoint(t *testing.T) {

	getEndpointTestCases := []struct {
		name           string
		mockStatus     int
		expectedStatus int
	}{
		{
			name:           "Hitting healthy GET endpoint",
			mockStatus:     http.StatusOK,
			expectedStatus: 200,
		},
		{
			name:           "Handling 500 Internal Error",
			mockStatus:     http.StatusInternalServerError,
			expectedStatus: 500,
		},
		{
			name:           "Handling 503 Service Unavailable",
			mockStatus:     http.StatusServiceUnavailable,
			expectedStatus: 503,
		},
		{
			name:           "Handling 504 Gateway Timeout",
			mockStatus:     http.StatusGatewayTimeout,
			expectedStatus: 504,
		},
	}

	for _, tc := range getEndpointTestCases {
		t.Run(tc.name, func(t *testing.T) {
			mockGetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tc.mockStatus)
			}))
			defer mockGetServer.Close()

			statusCode, err := HitGetEndpoint(mockGetServer.URL)

			if err != nil {
				t.Fatalf("expected no error, but got: %v", err)
			}

			if statusCode != tc.expectedStatus {
				t.Errorf("got %v status code, want %v status code", statusCode, tc.expectedStatus)
			}
		})
	}

}
