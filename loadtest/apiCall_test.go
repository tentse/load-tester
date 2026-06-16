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
				w.WriteHeader(tc.mockStatus)
			}))
			defer mockGetServer.Close()

			statusCode, err := HitGetEndpoint(mockGetServer.URL)

			if err != nil {
				t.Fatalf("expected no error, but got: %v", err)
			} else if statusCode != tc.expectedStatus {
				t.Errorf("got %v status code, want %v status code", statusCode, tc.expectedStatus)
			}
		})
	}

}

func TestPostEndpoint(t *testing.T) {

	mockPostTestCases := []struct {
		name           string
		mockStatus     int
		expectedStatus int
	}{
		{
			name:           "Handling 201 Created",
			mockStatus:     http.StatusCreated,
			expectedStatus: 201,
		},
		{
			name:           "Handling 204 No Content",
			mockStatus:     http.StatusNoContent,
			expectedStatus: 204,
		},
		{
			name:           "Handling 400 Bad Request",
			mockStatus:     http.StatusBadRequest,
			expectedStatus: 400,
		},
		{
			name:           "Handling 413 Content Too Large",
			mockStatus:     http.StatusRequestEntityTooLarge,
			expectedStatus: 413,
		},
		{
			name:           "Handling 415 Unsupported Media Type",
			mockStatus:     http.StatusUnsupportedMediaType,
			expectedStatus: 415,
		},
		{
			name:           "Handling 409 Conflict",
			mockStatus:     http.StatusConflict,
			expectedStatus: 409,
		},
		{
			name:           "Handling 429 Too Many Requests",
			mockStatus:     http.StatusTooManyRequests,
			expectedStatus: 429,
		},
		{
			name:           "Handling 500 internal server error",
			mockStatus:     http.StatusInternalServerError,
			expectedStatus: 500,
		},
	}

	for _, tc := range mockPostTestCases {
		t.Run(tc.name, func(t *testing.T) {
			mockPostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.WriteHeader(tc.mockStatus)
			}))
			defer mockPostServer.Close()

			statusCode, err := HitPostEndpoint(mockPostServer.URL)

			if err != nil {
				t.Fatalf("expected no error, but got: %v", err)
			} else if statusCode != tc.expectedStatus {
				t.Errorf("got %v status code, want %v status code", statusCode, tc.expectedStatus)
			}
		})
	}

}
