package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	}
	for _, tt := range tests {
		c := NewClient(tt.input, "tk_test")
		if c.baseURL != tt.want {
			t.Errorf("NewClient(%q).baseURL = %q, want %q", tt.input, c.baseURL, tt.want)
		}
	}
}

func TestClient_DoRaw_SetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_mytoken123")
	_, err := client.doRaw(context.Background(), "GET", "/projects", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tk_mytoken123" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tk_mytoken123")
	}
}

func TestClient_DoRaw_BuildsCorrectURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	_, err := client.doRaw(context.Background(), "GET", "/projects?page=2&per_page=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "/api/v2/projects?page=2&per_page=10"
	if gotPath != want {
		t.Errorf("request URI = %q, want %q", gotPath, want)
	}
}

func TestClient_DoRaw_SendsBody(t *testing.T) {
	var gotBody string
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(201)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	body := map[string]any{"title": "Test Task"}
	_, err := client.doRaw(context.Background(), "POST", "/projects/1/tasks", body)
	if err != nil {
		t.Fatal(err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(gotBody), &parsed); err != nil {
		t.Fatalf("invalid JSON body: %s", gotBody)
	}
	if parsed["title"] != "Test Task" {
		t.Errorf("body title = %v, want %q", parsed["title"], "Test Task")
	}
}

func TestClient_DoRaw_ReturnsVikunjaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]any{
			"status": 404,
			"title":  "Not Found",
			"detail": "This task does not exist.",
			"code":   4004,
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	_, err := client.doRaw(context.Background(), "GET", "/tasks/999", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	ve, ok := err.(*VikunjaError)
	if !ok {
		t.Fatalf("expected *VikunjaError, got %T: %v", err, err)
	}
	if ve.Status != 404 {
		t.Errorf("Status = %d, want 404", ve.Status)
	}
	if ve.Code != 4004 {
		t.Errorf("Code = %d, want 4004", ve.Code)
	}
	if !strings.Contains(ve.Error(), "This task does not exist") {
		t.Errorf("Error() = %q, want it to contain %q", ve.Error(), "This task does not exist")
	}
}

func TestClient_Do_DecodesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    42,
			"title": "My Project",
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	var result map[string]any
	err := client.do(context.Background(), "GET", "/projects/42", nil, &result)
	if err != nil {
		t.Fatal(err)
	}

	if result["title"] != "My Project" {
		t.Errorf("title = %v, want %q", result["title"], "My Project")
	}
}

func TestClient_Do_Handles204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	err := client.do(context.Background(), "DELETE", "/tasks/1", nil, nil)
	if err != nil {
		t.Fatalf("expected no error on 204, got: %v", err)
	}
}

func TestClient_Do_NoContentTypeOnGET(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tk_test")
	_, err := client.doRaw(context.Background(), "GET", "/projects", nil)
	if err != nil {
		t.Fatal(err)
	}

	if gotContentType != "" {
		t.Errorf("Content-Type on GET = %q, want empty", gotContentType)
	}
}

func TestTextResult(t *testing.T) {
	r := textResult([]byte(`{"id":1}`))
	if r.IsError {
		t.Error("expected IsError=false")
	}
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(r.Content))
	}
}

func TestErrorResult(t *testing.T) {
	r := errorResult(&VikunjaError{Status: 403, Title: "Forbidden", Detail: "no access", Code: 1001})
	if !r.IsError {
		t.Error("expected IsError=true")
	}
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(r.Content))
	}
}
