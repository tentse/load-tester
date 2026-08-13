package configfile

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

func fileWith(content string) fstest.MapFS {
	return fstest.MapFS{"requests.json": {Data: []byte(content)}}
}

func TestLoad(t *testing.T) {
	fsys := fileWith(`{
		"$schema": "https://example.com/schema.json",
		"version": 1,
		"baseUrl": "https://staging.example.com",
		"concurrency": 50,
		"timeout": "30s",
		"requests": [
			{ "name": "search", "url": "/search?q=foo",
			  "headers": { "X-Api-Key": "secret" },
			  "count": 40, "expectStatus": 200 },
			{ "name": "ingest", "method": "POST", "url": "/orders",
			  "body": { "sku": "A-100", "qty": 2 },
			  "count": 5, "expectStatus": 201 }
		]
	}`)

	got, err := Load(fsys, "requests.json")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := loadtest.FileConfig{
		Version:     1,
		BaseURL:     "https://staging.example.com",
		Concurrency: 50,
		Timeout:     30 * time.Second,
		Requests: []loadtest.RequestSpec{
			{
				Name:   "search",
				URL:    "/search?q=foo",
				Method: http.MethodGet,
				Header: http.Header{"X-Api-Key": {"secret"}},
				Count:  40,
				Expect: 200,
			},
			{
				Name:   "ingest",
				URL:    "/orders",
				Method: http.MethodPost,
				Header: http.Header{},
				Body:   `{ "sku": "A-100", "qty": 2 }`,
				Count:  5,
				Expect: 201,
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() =\n%+v\nwant:\n%+v", got, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	fsys := fileWith(`{
		"version": 1,
		"concurrency": 1,
		"timeout": "1s",
		"requests": [
			{ "name": "search", "url": "https://example.com/search?q=foo", "expectStatus": 200 }
		]
	}`)

	got, err := Load(fsys, "requests.json")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	spec := got.Requests[0]
	if spec.Method != http.MethodGet {
		t.Errorf("Method = %q, want %q", spec.Method, http.MethodGet)
	}
	if spec.Count != 1 {
		t.Errorf("Count = %d, want 1", spec.Count)
	}
}

func TestLoadTimeout(t *testing.T) {
	tests := []struct {
		name            string
		timeout         string
		want            time.Duration
		wantErrContains string
	}{
		{name: "duration string", timeout: `"30s"`, want: 30 * time.Second},
		{name: "milliseconds", timeout: `"1500ms"`, want: 1500 * time.Millisecond},
		{name: "bare number is not nanoseconds", timeout: `30`, wantErrContains: "invalid timeout"},
		{name: "unparseable", timeout: `"banana"`, wantErrContains: "invalid timeout"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fileWith(`{
				"version": 1, "concurrency": 1, "timeout": ` + tc.timeout + `,
				"requests": [{ "name": "alpha", "url": "https://example.com/", "expectStatus": 200 }]
			}`)

			got, err := Load(fsys, "requests.json")

			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want one containing %q", tc.wantErrContains)
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("Load() error = %q, want it to contain %q", err, tc.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if got.Timeout != tc.want {
				t.Errorf("Timeout = %v, want %v", got.Timeout, tc.want)
			}
		})
	}
}

func TestLoadCountZero(t *testing.T) {
	fsys := fileWith(`{
		"version": 1, "concurrency": 1, "timeout": "1s",
		"requests": [{ "name": "alpha", "url": "https://example.com/", "count": 0, "expectStatus": 200 }]
	}`)

	_, err := Load(fsys, "requests.json")
	if err == nil {
		t.Fatal("Load() error = nil, want an error: an explicit count of 0 must not default to 1")
	}
	if !strings.Contains(err.Error(), "invalid count") {
		t.Errorf("Load() error = %q, want it to mention count", err)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantErrContains string
	}{
		{
			name:            "version missing",
			content:         `{ "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/", "expectStatus": 200 }] }`,
			wantErrContains: "version is required",
		},
		{
			name:            "concurrency missing",
			content:         `{ "version": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/", "expectStatus": 200 }] }`,
			wantErrContains: "concurrency is required",
		},
		{
			name:            "timeout missing",
			content:         `{ "version": 1, "concurrency": 1, "requests": [{ "name": "alpha", "url": "https://e.com/", "expectStatus": 200 }] }`,
			wantErrContains: "timeout is required",
		},
		{
			name:            "expectStatus missing",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/" }] }`,
			wantErrContains: "requests[0].expectStatus",
		},
		{
			name:            "url missing with no baseUrl",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "expectStatus": 200 }] }`,
			wantErrContains: "requests[0].url is required",
		},
		{
			name:            "name missing",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [{ "url": "https://e.com/", "expectStatus": 200 }] }`,
			wantErrContains: "requests[0].name is required",
		},
		{
			name:            "name missing on the second entry",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/a", "expectStatus": 200 }, { "url": "https://e.com/b", "expectStatus": 200 }] }`,
			wantErrContains: "requests[1].name is required",
		},
		{
			name:            "requests empty",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [] }`,
			wantErrContains: "requests must hold at least one entry",
		},
		{
			name:            "version unsupported",
			content:         `{ "version": 2, "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/", "expectStatus": 200 }] }`,
			wantErrContains: "invalid version -> 2",
		},
		{
			name:            "expectStatus out of range",
			content:         `{ "version": 1, "concurrency": 1, "timeout": "1s", "requests": [{ "name": "alpha", "url": "https://e.com/", "expectStatus": 99999 }] }`,
			wantErrContains: "invalid expectStatus -> 99999",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(fileWith(tc.content), "requests.json")
			if err == nil {
				t.Fatalf("Load() error = nil, want one containing %q", tc.wantErrContains)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("Load() error = %q, want it to contain %q", err, tc.wantErrContains)
			}
		})
	}
}

func TestLoadUnknownField(t *testing.T) {
	fsys := fileWith(`{
		"version": 1, "concurrency": 1, "timeout": "1s",
		"requests": [{ "name": "alpha", "url": "https://example.com/", "expectStatuss": 200 }]
	}`)

	_, err := Load(fsys, "requests.json")
	if err == nil {
		t.Fatal("Load() error = nil, want an error for the misspelled key")
	}
	if !strings.Contains(err.Error(), "unknown field: expectStatuss") {
		t.Errorf("Load() error = %q, want it to name the unknown field", err)
	}
}

func TestLoadSchemaIgnored(t *testing.T) {
	fsys := fileWith(`{
		"$schema": "https://example.com/requests.schema.json",
		"version": 1, "concurrency": 1, "timeout": "1s",
		"requests": [{ "name": "alpha", "url": "https://example.com/", "expectStatus": 200 }]
	}`)

	if _, err := Load(fsys, "requests.json"); err != nil {
		t.Fatalf("Load() error = %v, want nil: $schema is for editors and must be accepted", err)
	}
}

func TestLoadBodyForms(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "object", body: `{"sku":"A-100"}`, want: `{"sku":"A-100"}`},
		{name: "array", body: `[1,2,3]`, want: `[1,2,3]`},
		{name: "string", body: `"already text"`, want: `already text`},
		{name: "absent", body: ``, want: ``},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := `{ "name": "alpha", "url": "https://example.com/", "expectStatus": 200`
			if tc.body != "" {
				entry += `, "body": ` + tc.body
			}
			entry += ` }`

			got, err := Load(fileWith(`{
				"version": 1, "concurrency": 1, "timeout": "1s",
				"requests": [`+entry+`]
			}`), "requests.json")
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if got.Requests[0].Body != tc.want {
				t.Errorf("Body = %q, want %q", got.Requests[0].Body, tc.want)
			}
		})
	}
}

func TestLoadHeaders(t *testing.T) {
	got, err := Load(fileWith(`{
		"version": 1, "concurrency": 1, "timeout": "1s",
		"requests": [{
			"name": "alpha", "url": "https://example.com/", "expectStatus": 200,
			"headers": { "X-Api-Key": "secret", "content-type": "application/json" }
		}]
	}`), "requests.json")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	header := got.Requests[0].Header
	if v := header.Get("X-Api-Key"); v != "secret" {
		t.Errorf("X-Api-Key = %q, want %q", v, "secret")
	}
	if v := header.Get("Content-Type"); v != "application/json" {
		t.Errorf("Content-Type = %q, want %q", v, "application/json")
	}
}

func TestLoadInvalidHeaderName(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "empty name", key: "", value: "x"},
		{name: "newline in name", key: "X-Ta\\ng", value: "x"},
		{name: "newline in value", key: "X-Tag", value: "a\\nb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(fileWith(`{
				"version": 1, "concurrency": 1, "timeout": "1s",
				"requests": [{
					"name": "alpha", "url": "https://example.com/", "expectStatus": 200,
					"headers": { "`+tc.key+`": "`+tc.value+`" }
				}]
			}`), "requests.json")

			if err == nil {
				t.Fatal("Load() error = nil, want an error for the invalid header")
			}
			if !strings.Contains(err.Error(), "header") {
				t.Errorf("Load() error = %q, want it to mention the header", err)
			}
		})
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load(fstest.MapFS{}, "missing.json")
	if err == nil {
		t.Fatal("Load() error = nil, want an error for the missing file")
	}
	if !strings.Contains(err.Error(), "missing.json") {
		t.Errorf("Load() error = %q, want it to name the file", err)
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	_, err := Load(fileWith(`{ "version": 1, }`), "requests.json")
	if err == nil {
		t.Fatal("Load() error = nil, want a syntax error")
	}
	if !strings.Contains(err.Error(), "requests.json") {
		t.Errorf("Load() error = %q, want it to name the file", err)
	}
}

func TestLoadErrorsAreInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		fsys    fstest.MapFS
		file    string
		content string
	}{
		{name: "missing file", fsys: fstest.MapFS{}, file: "missing.json"},
		{name: "malformed json", content: `{`},
		{name: "unknown field", content: `{ "version": 1, "nope": 1 }`},
		{name: "missing version", content: `{ "concurrency": 1, "timeout": "1s", "requests": [] }`},
		{name: "bad timeout", content: `{ "version": 1, "concurrency": 1, "timeout": 30, "requests": [] }`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys, name := tc.fsys, tc.file
			if tc.content != "" {
				fsys, name = fileWith(tc.content), "requests.json"
			}

			_, err := Load(fsys, name)
			if err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
			if !errors.Is(err, loadtest.ErrInvalidConfig) {
				t.Errorf("Load() error = %v, want it to wrap loadtest.ErrInvalidConfig", err)
			}
		})
	}
}

func TestLoadFeedsFileRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg, err := Load(fileWith(`{
		"version": 1,
		"baseUrl": "`+server.URL+`",
		"concurrency": 2,
		"timeout": "2s",
		"requests": [
			{ "name": "alpha", "url": "/a", "count": 3, "expectStatus": 200 },
			{ "name": "beta",  "url": "/b", "count": 2, "expectStatus": 200 }
		]
	}`), "requests.json")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got, err := loadtest.FileRun(t.Context(), cfg)
	if err != nil {
		t.Fatalf("FileRun() error = %v, want nil", err)
	}

	for name, wantTotal := range map[string]int{"alpha": 3, "beta": 2} {
		summary, ok := got[name]
		if !ok {
			t.Errorf("missing summary for %q", name)
			continue
		}
		if summary.Total != wantTotal {
			t.Errorf("%s total = %d, want %d", name, summary.Total, wantTotal)
		}
		if summary.Succeeded != wantTotal {
			t.Errorf("%s succeeded = %d, want %d", name, summary.Succeeded, wantTotal)
		}
	}
}
