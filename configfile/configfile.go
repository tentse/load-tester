// Package configfile reads a JSON file describing a multi-endpoint load test and turns it into a
// [loadtest.FileConfig].
//
// The file format and the runtime type are deliberately different shapes. The format allows
// optional fields with defaults, writes a timeout as a duration string, and lets a body be an
// object, an array or a string; [loadtest.FileConfig] has none of that, because a worker should
// never have to think about a JSON concern. Load is the step that turns one into the other, and
// it reports every problem it finds before a single request is sent.
//
// Every error wraps [loadtest.ErrInvalidConfig], so a caller can tell a bad file from a failed
// run with one check. Problems are reported by position, as requests[2].expectStatus rather than
// by field name alone, so a long file can be corrected without counting entries by hand.
package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/tentse/load-tester/loadtest"
)

const supportedVersion = 1

type file struct {
	Schema      string    `json:"$schema"`
	Version     *int      `json:"version"`
	BaseURL     string    `json:"baseUrl"`
	Concurrency *int      `json:"concurrency"`
	Timeout     *string   `json:"timeout"`
	Requests    []request `json:"requests"`
}

type request struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Body    json.RawMessage   `json:"body"`
	Headers map[string]string `json:"headers"`
	Count   *int              `json:"count"`
	Expect  *int              `json:"expectStatus"`
}

// Load reads name from fsys and returns a validated [loadtest.FileConfig].
//
// An fs.FS name cannot be absolute or contain "..", so split a user-supplied path first: the
// loadtester command passes os.DirFS of the directory, and the base name.
//
// Unknown fields are rejected; "$schema" and anything after the closing brace are ignored. Load
// stops at the first problem, and every error wraps [loadtest.ErrInvalidConfig] and names its
// position, as requests[2].expectStatus.
func Load(fsys fs.FS, name string) (loadtest.FileConfig, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return loadtest.FileConfig{}, fmt.Errorf("%w: read %s: %v", loadtest.ErrInvalidConfig, name, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var parsed file
	if err := decoder.Decode(&parsed); err != nil {
		return loadtest.FileConfig{}, fmt.Errorf("%w: %s: %v", loadtest.ErrInvalidConfig, name, decodeError(err))
	}

	cfg, err := convert(parsed)
	if err != nil {
		return loadtest.FileConfig{}, fmt.Errorf("%w: %s: %v", loadtest.ErrInvalidConfig, name, err)
	}

	return cfg, nil
}

func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("invalid %s: expected %s, got %s", typeErr.Field, typeErr.Type, typeErr.Value)
	}

	if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return fmt.Errorf("unknown field: %s", strings.Trim(field, `"`))
	}

	return err
}

func convert(parsed file) (loadtest.FileConfig, error) {
	if parsed.Version == nil {
		return loadtest.FileConfig{}, fmt.Errorf("version is required")
	}
	if *parsed.Version != supportedVersion {
		return loadtest.FileConfig{}, fmt.Errorf("invalid version -> %d, want %d", *parsed.Version, supportedVersion)
	}
	if parsed.Concurrency == nil {
		return loadtest.FileConfig{}, fmt.Errorf("concurrency is required")
	}
	if *parsed.Concurrency <= 0 {
		return loadtest.FileConfig{}, fmt.Errorf("invalid concurrency -> %d", *parsed.Concurrency)
	}
	if parsed.Timeout == nil {
		return loadtest.FileConfig{}, fmt.Errorf("timeout is required")
	}

	timeout, err := time.ParseDuration(*parsed.Timeout)
	if err != nil {
		return loadtest.FileConfig{}, fmt.Errorf("invalid timeout -> %q, want a duration such as \"30s\"", *parsed.Timeout)
	}
	if timeout <= 0 {
		return loadtest.FileConfig{}, fmt.Errorf("invalid timeout -> %v", timeout)
	}
	if len(parsed.Requests) == 0 {
		return loadtest.FileConfig{}, fmt.Errorf("requests must hold at least one entry")
	}

	specs := make([]loadtest.RequestSpec, 0, len(parsed.Requests))
	for index, entry := range parsed.Requests {
		spec, err := convertRequest(index, entry, parsed.BaseURL)
		if err != nil {
			return loadtest.FileConfig{}, err
		}
		specs = append(specs, spec)
	}

	return loadtest.FileConfig{
		Version:     *parsed.Version,
		BaseURL:     parsed.BaseURL,
		Concurrency: *parsed.Concurrency,
		Timeout:     timeout,
		Requests:    specs,
	}, nil
}

func convertRequest(index int, entry request, baseURL string) (loadtest.RequestSpec, error) {
	where := fmt.Sprintf("requests[%d]", index)

	if entry.Name == "" {
		return loadtest.RequestSpec{}, fmt.Errorf("%s.name is required", where)
	}
	if entry.URL == "" && baseURL == "" {
		return loadtest.RequestSpec{}, fmt.Errorf("%s.url is required when baseUrl is not set", where)
	}
	if entry.Expect == nil {
		return loadtest.RequestSpec{}, fmt.Errorf("%s.expectStatus is required", where)
	}
	if *entry.Expect < 100 || *entry.Expect > 599 {
		return loadtest.RequestSpec{}, fmt.Errorf("%s: invalid expectStatus -> %d, want 100 to 599", where, *entry.Expect)
	}

	count := 1
	if entry.Count != nil {
		if *entry.Count <= 0 {
			return loadtest.RequestSpec{}, fmt.Errorf("%s: invalid count -> %d", where, *entry.Count)
		}
		count = *entry.Count
	}

	method := entry.Method
	if method == "" {
		method = http.MethodGet
	}

	header, err := convertHeaders(entry.Headers)
	if err != nil {
		return loadtest.RequestSpec{}, fmt.Errorf("%s.%v", where, err)
	}

	body, err := convertBody(entry.Body)
	if err != nil {
		return loadtest.RequestSpec{}, fmt.Errorf("%s.%v", where, err)
	}

	return loadtest.RequestSpec{
		Name:   entry.Name,
		URL:    entry.URL,
		Method: method,
		Header: header,
		Body:   body,
		Count:  count,
		Expect: *entry.Expect,
	}, nil
}

func convertHeaders(headers map[string]string) (http.Header, error) {
	out := http.Header{}

	for name, value := range headers {
		if name == "" {
			return nil, fmt.Errorf("headers has an entry with an empty name")
		}
		if strings.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("headers[%q] name contains a whitespace character", name)
		}
		if strings.ContainsAny(name+value, "\r\n") {
			return nil, fmt.Errorf("headers[%q] contains CR or LF", name)
		}
		out.Add(name, value)
	}

	return out, nil
}

func convertBody(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("body is not valid JSON")
		}
		return s, nil
	}

	return string(raw), nil
}
