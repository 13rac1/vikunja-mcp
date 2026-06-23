package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewClient_NormalizesURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://vikunja.example.com", "https://vikunja.example.com/api/v2"},
		{"https://vikunja.example.com/", "https://vikunja.example.com/api/v2"},
		{"https://vikunja.example.com/api/v2", "https://vikunja.example.com/api/v2"},
		{"https://vikunja.example.com/api/v2/", "https://vikunja.example.com/api/v2"},
		{"http://localhost:3456", "http://localhost:3456/api/v2"},
		{"http://localhost:3456/", "http://localhost:3456/api/v2"},
	}
	for _, tt := range tests {
		c := NewClient(tt.input, "tk_test")
		if c.baseURL != tt.want {
			t.Errorf("NewClient(%q).baseURL = %q, want %q", tt.input, c.baseURL, tt.want)
		}
	}
}

func TestVikunjaError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  VikunjaError
		want string
	}{
		{
			name: "with detail",
			err:  VikunjaError{Status: 404, Title: "Not Found", Detail: "This task does not exist.", Code: 4004},
			want: "Not Found: This task does not exist. (HTTP 404, code 4004)",
		},
		{
			name: "without detail",
			err:  VikunjaError{Status: 500, Title: "Internal Server Error", Code: 0},
			want: "Internal Server Error (HTTP 500, code 0)",
		},
		{
			name: "permission denied",
			err:  VikunjaError{Status: 403, Title: "Forbidden", Detail: "Insufficient permissions", Code: 1001},
			want: "Forbidden: Insufficient permissions (HTTP 403, code 1001)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPageQuery(t *testing.T) {
	tests := []struct {
		name    string
		search  string
		page    int
		perPage int
		want    string
	}{
		{"empty", "", 0, 0, ""},
		{"search only", "hello", 0, 0, "q=hello"},
		{"page only", "", 2, 0, "page=2"},
		{"per_page only", "", 0, 50, "per_page=50"},
		{"all fields", "test", 3, 25, "page=3&per_page=25&q=test"},
		{"search with spaces", "my task", 1, 10, "page=1&per_page=10&q=my+task"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPageQuery(tt.search, tt.page, tt.perPage).Encode()
			if got != tt.want {
				t.Errorf("buildPageQuery(%q, %d, %d) = %q, want %q",
					tt.search, tt.page, tt.perPage, got, tt.want)
			}
		})
	}
}

func TestAppendQuery(t *testing.T) {
	t.Run("empty values", func(t *testing.T) {
		got := appendQuery("/tasks", buildPageQuery("", 0, 0))
		if got != "/tasks" {
			t.Errorf("got %q, want %q", got, "/tasks")
		}
	})
	t.Run("with search", func(t *testing.T) {
		got := appendQuery("/tasks", buildPageQuery("hello", 0, 0))
		if got != "/tasks?q=hello" {
			t.Errorf("got %q, want %q", got, "/tasks?q=hello")
		}
	})
	t.Run("empty path with values", func(t *testing.T) {
		got := appendQuery("", buildPageQuery("test", 0, 0))
		if got != "?q=test" {
			t.Errorf("got %q, want %q", got, "?q=test")
		}
	})
}

func TestBuildTaskListQuery(t *testing.T) {
	tests := []struct {
		name               string
		search             string
		filter             string
		filterTimezone     string
		filterIncludeNulls bool
		sortBy             []string
		orderBy            []string
		page               int
		perPage            int
		wantContains       []string
		wantEmpty          bool
	}{
		{
			name:      "empty",
			wantEmpty: true,
		},
		{
			name:         "search only",
			search:       "groceries",
			wantContains: []string{"q=groceries"},
		},
		{
			name:         "filter",
			filter:       "done = false",
			wantContains: []string{"filter=done+%3D+false"},
		},
		{
			name:           "filter with timezone",
			filter:         "due_date < now",
			filterTimezone: "Europe/Berlin",
			wantContains:   []string{"filter=", "filter_timezone=Europe%2FBerlin"},
		},
		{
			name:               "filter include nulls",
			filter:             "due_date > now",
			filterIncludeNulls: true,
			wantContains:       []string{"filter_include_nulls=true"},
		},
		{
			name:         "sort",
			sortBy:       []string{"priority", "due_date"},
			orderBy:      []string{"desc", "asc"},
			wantContains: []string{"sort_by=priority", "sort_by=due_date", "order_by=desc", "order_by=asc"},
		},
		{
			name:         "pagination",
			page:         2,
			perPage:      25,
			wantContains: []string{"page=2", "per_page=25"},
		},
		{
			name:         "combined",
			search:       "test",
			filter:       "done = false",
			page:         1,
			perPage:      50,
			wantContains: []string{"q=test", "filter=", "page=1", "per_page=50"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTaskListQuery(tt.search, tt.filter, tt.filterTimezone,
				tt.filterIncludeNulls, tt.sortBy, tt.orderBy, tt.page, tt.perPage)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
				return
			}
			if got == "" {
				t.Fatal("got empty, want non-empty")
			}
			if got[0] != '?' {
				t.Errorf("got %q, want leading '?'", got)
			}
			for _, want := range tt.wantContains {
				if !containsSubstring(got, want) {
					t.Errorf("query %q missing expected substring %q", got, want)
				}
			}
		})
	}
}

// containsSubstring is a helper to avoid importing strings just for tests.
func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// requestLog captures what the mock server received.
type requestLog struct {
	Method      string
	Path        string
	Auth        string
	ContentType string
	Body        string
}

// newMockAPI creates an httptest.Server that records each request and responds
// with the given status and body. Returns the server and a pointer to the last
// recorded request.
func newMockAPI(t *testing.T, status int, responseBody string) (*httptest.Server, *requestLog) {
	t.Helper()
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Method = r.Method
		log.Path = r.URL.RequestURI()
		log.Auth = r.Header.Get("Authorization")
		log.ContentType = r.Header.Get("Content-Type")
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		log.Body = string(b)
		w.WriteHeader(status)
		if responseBody != "" {
			if _, err := w.Write([]byte(responseBody)); err != nil {
				t.Errorf("writing response: %v", err)
			}
		}
	}))
	return srv, &log
}

func TestDoRaw_RequestConstruction(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       any
		status     int
		response   string
		wantMethod string
		wantPath   string
		wantCT     string // expected Content-Type
		wantBody   string // substring expected in body, or "" for no body
	}{
		{
			name:       "GET without body",
			method:     "GET",
			path:       "/projects",
			body:       nil,
			status:     200,
			response:   `{"items":[]}`,
			wantMethod: "GET",
			wantPath:   "/api/v2/projects",
			wantCT:     "",
		},
		{
			name:       "GET with query params",
			method:     "GET",
			path:       "/tasks?q=hello&page=2",
			body:       nil,
			status:     200,
			response:   `{"items":[]}`,
			wantMethod: "GET",
			wantPath:   "/api/v2/tasks?q=hello&page=2",
			wantCT:     "",
		},
		{
			name:       "POST with body",
			method:     "POST",
			path:       "/projects/1/tasks",
			body:       map[string]any{"title": "Buy milk"},
			status:     201,
			response:   `{"id":1,"title":"Buy milk"}`,
			wantMethod: "POST",
			wantPath:   "/api/v2/projects/1/tasks",
			wantCT:     "application/json",
			wantBody:   `"title":"Buy milk"`,
		},
		{
			name:       "PATCH with partial body",
			method:     "PATCH",
			path:       "/tasks/42",
			body:       map[string]any{"done": true},
			status:     200,
			response:   `{"id":42,"done":true}`,
			wantMethod: "PATCH",
			wantPath:   "/api/v2/tasks/42",
			wantCT:     "application/json",
			wantBody:   `"done":true`,
		},
		{
			name:       "DELETE returns 204",
			method:     "DELETE",
			path:       "/tasks/5",
			body:       nil,
			status:     204,
			response:   "",
			wantMethod: "DELETE",
			wantPath:   "/api/v2/tasks/5",
			wantCT:     "",
		},
		{
			name:       "POST with struct body",
			method:     "POST",
			path:       "/labels",
			body:       CreateLabelInput{Title: "urgent", HexColor: "ff0000"},
			status:     201,
			response:   `{"id":1,"title":"urgent"}`,
			wantMethod: "POST",
			wantPath:   "/api/v2/labels",
			wantCT:     "application/json",
			wantBody:   `"title":"urgent"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, log := newMockAPI(t, tt.status, tt.response)
			defer srv.Close()

			client := NewClient(srv.URL, "tk_abc")
			data, err := client.doRaw(context.Background(), tt.method, tt.path, tt.body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if log.Method != tt.wantMethod {
				t.Errorf("method = %q, want %q", log.Method, tt.wantMethod)
			}
			if log.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", log.Path, tt.wantPath)
			}
			if log.Auth != "Bearer tk_abc" {
				t.Errorf("auth = %q, want %q", log.Auth, "Bearer tk_abc")
			}
			if log.ContentType != tt.wantCT {
				t.Errorf("content-type = %q, want %q", log.ContentType, tt.wantCT)
			}
			if tt.wantBody != "" && !containsSubstring(log.Body, tt.wantBody) {
				t.Errorf("body = %q, want substring %q", log.Body, tt.wantBody)
			}
			// 204 returns nil data.
			if tt.status == 204 && data != nil {
				t.Errorf("expected nil data for 204, got %q", data)
			}
			if tt.status != 204 && data == nil {
				t.Error("expected non-nil data, got nil")
			}
		})
	}
}

func TestDoRaw_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantCode   int
		wantDetail string
	}{
		{
			name:       "404 task not found",
			status:     404,
			body:       `{"status":404,"title":"Not Found","detail":"This task does not exist.","code":4004}`,
			wantStatus: 404,
			wantCode:   4004,
			wantDetail: "This task does not exist.",
		},
		{
			name:       "403 forbidden",
			status:     403,
			body:       `{"status":403,"title":"Forbidden","detail":"Insufficient permissions","code":1001}`,
			wantStatus: 403,
			wantCode:   1001,
			wantDetail: "Insufficient permissions",
		},
		{
			name:       "401 unauthorized",
			status:     401,
			body:       `{"status":401,"title":"Unauthorized","detail":"invalid or missing authentication","code":0}`,
			wantStatus: 401,
			wantCode:   0,
			wantDetail: "invalid or missing authentication",
		},
		{
			name:       "422 validation error",
			status:     422,
			body:       `{"status":422,"title":"Unprocessable Entity","detail":"title: non zero value required","code":4000}`,
			wantStatus: 422,
			wantCode:   4000,
			wantDetail: "title: non zero value required",
		},
		{
			name:       "500 server error",
			status:     500,
			body:       `{"status":500,"title":"Internal Server Error","detail":"","code":0}`,
			wantStatus: 500,
			wantCode:   0,
			wantDetail: "",
		},
		{
			name:       "status inferred from HTTP when body has 0",
			status:     409,
			body:       `{"status":0,"title":"Conflict","detail":"duplicate","code":0}`,
			wantStatus: 409,
			wantCode:   0,
			wantDetail: "duplicate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newMockAPI(t, tt.status, tt.body)
			defer srv.Close()

			client := NewClient(srv.URL, "tk_test")
			_, err := client.doRaw(context.Background(), "GET", "/anything", nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var ve *VikunjaError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *VikunjaError, got %T: %v", err, err)
			}
			if ve.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", ve.Status, tt.wantStatus)
			}
			if ve.Code != tt.wantCode {
				t.Errorf("Code = %d, want %d", ve.Code, tt.wantCode)
			}
			if ve.Detail != tt.wantDetail {
				t.Errorf("Detail = %q, want %q", ve.Detail, tt.wantDetail)
			}
		})
	}
}

func TestDoRaw_NonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		if _, err := w.Write([]byte("Bad Gateway")); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	_, err := client.doRaw(context.Background(), "GET", "/anything", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should not be a VikunjaError since the body isn't JSON.
	var ve *VikunjaError
	if errors.As(err, &ve) {
		t.Error("expected non-VikunjaError for non-JSON response")
	}
	if !containsSubstring(err.Error(), "502") {
		t.Errorf("error should mention status code 502: %v", err)
	}
}

func TestDo_DecodesIntoStruct(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		wantID   float64
		wantName string
	}{
		{
			name:     "project",
			status:   200,
			response: `{"id":42,"title":"My Project"}`,
			wantID:   42,
			wantName: "My Project",
		},
		{
			name:     "task",
			status:   200,
			response: `{"id":7,"title":"Buy groceries"}`,
			wantID:   7,
			wantName: "Buy groceries",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newMockAPI(t, tt.status, tt.response)
			defer srv.Close()

			client := NewClient(srv.URL, "tk_test")
			var result map[string]any
			err := client.do(context.Background(), "GET", "/anything", nil, &result)
			if err != nil {
				t.Fatal(err)
			}
			if result["id"] != tt.wantID {
				t.Errorf("id = %v, want %v", result["id"], tt.wantID)
			}
			if result["title"] != tt.wantName {
				t.Errorf("title = %v, want %q", result["title"], tt.wantName)
			}
		})
	}
}

func TestDo_PropagatesErrors(t *testing.T) {
	srv, _ := newMockAPI(t, 404, `{"status":404,"title":"Not Found","detail":"gone","code":4004}`)
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	var result map[string]any
	err := client.do(context.Background(), "GET", "/tasks/999", nil, &result)
	if err == nil {
		t.Fatal("expected error")
	}
	var ve *VikunjaError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *VikunjaError, got %T", err)
	}
	if ve.Code != 4004 {
		t.Errorf("Code = %d, want 4004", ve.Code)
	}
}

func TestDo_NilResult(t *testing.T) {
	srv, _ := newMockAPI(t, 200, `{"id":1}`)
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	err := client.do(context.Background(), "POST", "/projects", map[string]any{"title": "x"}, nil)
	if err != nil {
		t.Fatalf("expected no error when result is nil, got: %v", err)
	}
}

func TestDoRaw_StructOmitsEmptyFields(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantKeys   []string
		wantAbsent []string
	}{
		{
			name:       "CreateLabelInput with only title",
			body:       CreateLabelInput{Title: "bug"},
			wantKeys:   []string{"title"},
			wantAbsent: []string{"description", "hex_color"},
		},
		{
			name:       "CreateLabelInput with all fields",
			body:       CreateLabelInput{Title: "urgent", Description: "fix now", HexColor: "ff0000"},
			wantKeys:   []string{"title", "description", "hex_color"},
			wantAbsent: []string{},
		},
		{
			name:       "CreateProjectInput minimal",
			body:       CreateProjectInput{Title: "My Project"},
			wantKeys:   []string{"title"},
			wantAbsent: []string{"description", "parent_project_id", "identifier", "hex_color"},
		},
		{
			name:       "CreateTaskInput minimal",
			body:       CreateTaskInput{ProjectID: 1, Title: "Do thing"},
			wantKeys:   []string{"title", "project_id"},
			wantAbsent: []string{"description", "priority", "due_date", "hex_color"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("reading request body: %v", err)
				}
				gotBody = string(b)
				w.WriteHeader(201)
				if _, err := w.Write([]byte(`{"id":1}`)); err != nil {
					t.Errorf("writing response: %v", err)
				}
			}))
			defer srv.Close()

			client := NewClient(srv.URL, "tk_test")
			_, err := client.doRaw(context.Background(), "POST", "/test", tt.body)
			if err != nil {
				t.Fatal(err)
			}

			var parsed map[string]any
			if err := json.Unmarshal([]byte(gotBody), &parsed); err != nil {
				t.Fatalf("invalid JSON: %s", gotBody)
			}
			for _, key := range tt.wantKeys {
				if _, ok := parsed[key]; !ok {
					t.Errorf("expected key %q in body, got: %s", key, gotBody)
				}
			}
			for _, key := range tt.wantAbsent {
				if _, ok := parsed[key]; ok {
					t.Errorf("unexpected key %q in body, got: %s", key, gotBody)
				}
			}
		})
	}
}

func TestTextResult(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"object", `{"id":1,"title":"test"}`},
		{"array", `[{"id":1},{"id":2}]`},
		{"empty object", `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := textResult([]byte(tt.raw))
			if r.IsError {
				t.Error("expected IsError=false")
			}
			if len(r.Content) != 1 {
				t.Fatalf("expected 1 content block, got %d", len(r.Content))
			}
			tc, ok := r.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("expected *mcp.TextContent, got %T", r.Content[0])
			}
			if tc.Text != tt.raw {
				t.Errorf("Text = %q, want %q", tc.Text, tt.raw)
			}
		})
	}
}

func TestErrorResult(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "vikunja error",
			err:     &VikunjaError{Status: 403, Title: "Forbidden", Detail: "no access", Code: 1001},
			wantMsg: "Forbidden: no access (HTTP 403, code 1001)",
		},
		{
			name:    "vikunja error without detail",
			err:     &VikunjaError{Status: 500, Title: "Internal Server Error", Code: 0},
			wantMsg: "Internal Server Error (HTTP 500, code 0)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := errorResult(tt.err)
			if !r.IsError {
				t.Error("expected IsError=true")
			}
			if len(r.Content) != 1 {
				t.Fatalf("expected 1 content block, got %d", len(r.Content))
			}
			tc, ok := r.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("expected *mcp.TextContent, got %T", r.Content[0])
			}
			if tc.Text != tt.wantMsg {
				t.Errorf("Text = %q, want %q", tc.Text, tt.wantMsg)
			}
		})
	}
}

func TestFilterJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		whitelist map[string]bool
		want      string
	}{
		{
			name:      "single object strips unknown fields",
			input:     `{"id":1,"title":"test","position":42,"subscription":null}`,
			whitelist: map[string]bool{"id": true, "title": true},
			want:      `{"id":1,"title":"test"}`,
		},
		{
			name:      "array of objects",
			input:     `[{"id":1,"title":"a","junk":true},{"id":2,"title":"b","junk":false}]`,
			whitelist: map[string]bool{"id": true, "title": true},
			want:      `[{"id":1,"title":"a"},{"id":2,"title":"b"}]`,
		},
		{
			name:      "empty array",
			input:     `[]`,
			whitelist: map[string]bool{"id": true},
			want:      `[]`,
		},
		{
			name:      "empty object",
			input:     `{}`,
			whitelist: map[string]bool{"id": true},
			want:      `{}`,
		},
		{
			name:      "empty input",
			input:     ``,
			whitelist: map[string]bool{"id": true},
			want:      ``,
		},
		{
			name:      "whitespace input",
			input:     `  `,
			whitelist: map[string]bool{"id": true},
			want:      ``,
		},
		{
			name:  "nested user object filtered",
			input: `{"id":1,"title":"task","created_by":{"id":5,"username":"bob","email":"bob@test.com","created":"2024-01-01"}}`,
			whitelist: map[string]bool{
				"id": true, "title": true, "created_by": true,
			},
			want: `{"created_by":{"id":5,"username":"bob"},"id":1,"title":"task"}`,
		},
		{
			name:  "nested array of users filtered",
			input: `{"id":1,"assignees":[{"id":1,"username":"alice","email":"a@b.com"},{"id":2,"username":"bob","email":"c@d.com"}]}`,
			whitelist: map[string]bool{
				"id": true, "assignees": true,
			},
			want: `{"assignees":[{"id":1,"username":"alice"},{"id":2,"username":"bob"}],"id":1}`,
		},
		{
			name:  "nested labels filtered",
			input: `{"id":1,"labels":[{"id":10,"title":"bug","hex_color":"ff0000","created_by":{"id":1,"username":"admin","email":"a@b.com"},"created":"2024-01-01"}]}`,
			whitelist: map[string]bool{
				"id": true, "labels": true,
			},
			want: `{"id":1,"labels":[{"created":"2024-01-01","created_by":{"id":1,"username":"admin"},"hex_color":"ff0000","id":10,"title":"bug"}]}`,
		},
		{
			name:      "null nested field preserved",
			input:     `{"id":1,"created_by":null}`,
			whitelist: map[string]bool{"id": true, "created_by": true},
			want:      `{"created_by":null,"id":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filterJSON([]byte(tt.input), tt.whitelist)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Normalize by re-marshaling expected JSON for comparison.
			if tt.want == "" || tt.want == "[]" || tt.want == "{}" {
				if string(got) != tt.want {
					t.Errorf("got %q, want %q", string(got), tt.want)
				}
				return
			}
			// Compare as JSON to ignore key ordering.
			var wantAny, gotAny any
			if unmarshalErr := json.Unmarshal([]byte(tt.want), &wantAny); unmarshalErr != nil {
				t.Fatalf("invalid want JSON: %v", unmarshalErr)
			}
			if unmarshalErr := json.Unmarshal(got, &gotAny); unmarshalErr != nil {
				t.Fatalf("invalid got JSON: %v", unmarshalErr)
			}
			wantBytes, marshalErr := json.Marshal(wantAny)
			if marshalErr != nil {
				t.Fatalf("marshaling want: %v", marshalErr)
			}
			gotBytes, marshalErr := json.Marshal(gotAny)
			if marshalErr != nil {
				t.Fatalf("marshaling got: %v", marshalErr)
			}
			if !bytes.Equal(gotBytes, wantBytes) {
				t.Errorf("got  %s\nwant %s", string(gotBytes), string(wantBytes))
			}
		})
	}
}

func TestFilterJSON_InvalidJSON(t *testing.T) {
	_, err := filterJSON([]byte(`{not json`), taskFields)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFilteredResult(t *testing.T) {
	raw := []byte(`{"id":1,"title":"test","position":42,"subscription":null}`)
	r := filteredResult(raw, map[string]bool{"id": true, "title": true})
	if r.IsError {
		t.Error("expected IsError=false")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", r.Content[0])
	}
	if containsSubstring(tc.Text, "position") {
		t.Errorf("filtered result should not contain 'position': %s", tc.Text)
	}
	if !containsSubstring(tc.Text, `"id":1`) {
		t.Errorf("filtered result should contain id: %s", tc.Text)
	}
}

func TestFilteredResult_FallbackOnInvalidJSON(t *testing.T) {
	raw := []byte(`{invalid}`)
	r := filteredResult(raw, taskFields)
	if r.IsError {
		t.Error("expected IsError=false (fallback to unfiltered)")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", r.Content[0])
	}
	if tc.Text != string(raw) {
		t.Errorf("expected fallback to raw, got %q", tc.Text)
	}
}

func TestDeleteResult(t *testing.T) {
	r := deleteResult()
	if r.IsError {
		t.Error("expected IsError=false")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", r.Content[0])
	}
	if tc.Text != "deleted" {
		t.Errorf("Text = %q, want %q", tc.Text, "deleted")
	}
}
