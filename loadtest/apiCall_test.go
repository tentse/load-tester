package loadtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetEndpoint(t *testing.T) {

	getEndpointTestCases := []struct {
		name           string
		token          string
		wantAuth       string
		mockStatus     int
		expectedStatus int
	}{
		{
			name:           "Hitting healthy GET endpoint",
			mockStatus:     http.StatusOK,
			token:          "",
			wantAuth:       "",
			expectedStatus: 200,
		},
		{
			name:           "Hitting GET with valid token",
			mockStatus:     http.StatusOK,
			token:          "token",
			wantAuth:       "Bearer token",
			expectedStatus: 200,
		},
		{
			name:           "Handling 403 forbidden",
			mockStatus:     http.StatusForbidden,
			token:          "user-token",
			wantAuth:       "Bearer user-token",
			expectedStatus: 403,
		},
		{
			name:           "Handling 500 Internal Error",
			mockStatus:     http.StatusInternalServerError,
			token:          "",
			wantAuth:       "",
			expectedStatus: 500,
		},
	}

	for _, tc := range getEndpointTestCases {
		t.Run(tc.name, func(t *testing.T) {
			mockGetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				} else if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
				}
				w.WriteHeader(tc.mockStatus)
			}))
			defer mockGetServer.Close()

			statusCode, err := HitGetEndpoint(mockGetServer.URL, tc.token)

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
		token          string
		wantAuth       string
		reqBody        string
		mockStatus     int
		expectedStatus int
	}{
		{
			name:           "Handling 201 Created",
			mockStatus:     http.StatusCreated,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 201,
		},
		{
			name:           "Handling 400 Unprocessable Content",
			mockStatus:     http.StatusBadRequest,
			token:          "token",
			wantAuth:       "Bearer token",
			reqBody:        `{body: 44`,
			expectedStatus: 400,
		},
		{
			name:           "Handling 204 No Content",
			mockStatus:     http.StatusNoContent,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 204,
		},
		{
			name:           "Handling 400 Bad Request",
			mockStatus:     http.StatusBadRequest,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 400,
		},
		{
			name:           "Handling 401 Unauthorized",
			mockStatus:     http.StatusUnauthorized,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 401,
		},
		{
			name:           "Handling 403 Forbidden",
			mockStatus:     http.StatusForbidden,
			token:          "token",
			wantAuth:       "Bearer token",
			reqBody:        `{body: "body"}`,
			expectedStatus: 403,
		},
		{
			name:           "Handling 404 Not Found",
			mockStatus:     http.StatusNotFound,
			token:          "token",
			wantAuth:       "Bearer token",
			reqBody:        `{body: "body"}`,
			expectedStatus: 404,
		},
		{
			name:           "Handling 405 Method Not Allowed",
			mockStatus:     http.StatusMethodNotAllowed,
			token:          "token",
			wantAuth:       "Bearer token",
			reqBody:        `{body: "body"}`,
			expectedStatus: 405,
		},
		{
			name:           "Handling 409 Conflict",
			mockStatus:     http.StatusConflict,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 409,
		},
		{
			name:           "Handling 413 Content Too Large",
			mockStatus:     http.StatusRequestEntityTooLarge,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 413,
		},
		{
			name:           "Handling 415 Unsupported Media Type",
			mockStatus:     http.StatusUnsupportedMediaType,
			token:          "",
			wantAuth:       "",
			reqBody:        `{media: "unsuported media"}`,
			expectedStatus: 415,
		},
		{
			name:           "Handling 422 Unprocessable Content",
			mockStatus:     http.StatusUnprocessableEntity,
			token:          "token",
			wantAuth:       "Bearer token",
			reqBody:        `{body: 44}`,
			expectedStatus: 422,
		},
		{
			name:           "Handling 429 Too Many Requests",
			mockStatus:     http.StatusTooManyRequests,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 429,
		},
		{
			name:           "Handling 500 internal server error",
			mockStatus:     http.StatusInternalServerError,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 500,
		},
		{
			name:           "Handling 503 Service Unavalable",
			mockStatus:     http.StatusServiceUnavailable,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 503,
		},
		{
			name:           "Handling 504 Gateway Timeout",
			mockStatus:     http.StatusGatewayTimeout,
			token:          "",
			wantAuth:       "",
			reqBody:        `{body: "body"}`,
			expectedStatus: 504,
		},
	}

	for _, tc := range mockPostTestCases {
		t.Run(tc.name, func(t *testing.T) {
			mockPostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				} else if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Errorf("Authorization = %q want %q", got, tc.wantAuth)
				} else if tc.reqBody != "" {
					body, _ := io.ReadAll(r.Body)
					if string(body) != tc.reqBody {
						t.Errorf("body = %q, want %q", body, tc.reqBody)
					}
				}
				w.WriteHeader(tc.mockStatus)
			}))
			defer mockPostServer.Close()

			statusCode, err := HitPostEndpoint(mockPostServer.URL, tc.token, tc.reqBody)

			if err != nil {
				t.Fatalf("expected no error, but got: %v", err)
			} else if statusCode != tc.expectedStatus {
				t.Errorf("got %v status code, want %v status code", statusCode, tc.expectedStatus)
			}
		})
	}

}
