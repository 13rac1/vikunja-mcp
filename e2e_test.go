//go:build e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	vikunjaURL  = "http://localhost:3456"
	composeFile = "compose.e2e.yml"
)

var testSession *mcp.ClientSession

func TestMain(m *testing.M) {
	ctx := context.Background()

	if err := composeUp(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "compose up: %v\n", err)
		os.Exit(1)
	}

	if err := waitForHealthy(ctx, vikunjaURL+"/api/v1/info", 60*time.Second); err != nil {
		composeDown(context.Background())
		fmt.Fprintf(os.Stderr, "vikunja not ready: %v\n", err)
		os.Exit(1)
	}

	token, err := setupAuth(ctx, vikunjaURL)
	if err != nil {
		composeDown(context.Background())
		fmt.Fprintf(os.Stderr, "auth setup: %v\n", err)
		os.Exit(1)
	}

	session, cleanup, err := setupMCP(ctx, vikunjaURL, token)
	if err != nil {
		composeDown(context.Background())
		fmt.Fprintf(os.Stderr, "mcp setup: %v\n", err)
		os.Exit(1)
	}
	testSession = session

	code := m.Run()

	cleanup()
	composeDown(context.Background())
	os.Exit(code)
}

func composeUp(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", composeFile, "up", "-d")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func composeDown(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "podman", "compose", "-f", composeFile, "down", "-v")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	_ = cmd.Run() //nolint:errcheck // best-effort cleanup
}

func waitForHealthy(ctx context.Context, healthURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, http.NoBody)
		if err != nil {
			return fmt.Errorf("creating health check request: %w", err)
		}
		resp, err := httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("timed out after %s waiting for %s", timeout, healthURL)
}

func setupAuth(ctx context.Context, baseURL string) (string, error) {
	// Register a user.
	regBody := map[string]string{
		"username": "e2etest",
		"password": "e2etestpass",
		"email":    "e2e@test.local",
	}
	if err := postJSON(ctx, baseURL+"/api/v1/register", "", regBody, nil); err != nil {
		return "", fmt.Errorf("register: %w", err)
	}

	// Login to get a JWT.
	loginBody := map[string]string{
		"username": "e2etest",
		"password": "e2etestpass",
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := postJSON(ctx, baseURL+"/api/v1/login", "", loginBody, &loginResp); err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if loginResp.Token == "" {
		return "", fmt.Errorf("login returned empty token")
	}

	// Create a scoped API token using the JWT.
	tokenBody := map[string]any{
		"title": "e2e-mcp",
		"permissions": map[string][]string{
			"projects":        {"read_all", "read_one", "create"},
			"tasks":           {"read_all", "read_one", "create", "update", "delete"},
			"labels":          {"read_all", "create"},
			"tasks_labels":    {"create", "delete"},
			"tasks_comments":  {"read_all", "create"},
			"tasks_assignees": {"create", "delete"},
			"time-entries":    {"read_all", "read_one", "create", "update", "delete"},
		},
		"expires_at": "2099-01-01T00:00:00Z",
	}
	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := postJSON(ctx, baseURL+"/api/v2/tokens", loginResp.Token, tokenBody, &tokenResp); err != nil {
		return "", fmt.Errorf("create api token: %w", err)
	}
	if tokenResp.Token == "" {
		return "", fmt.Errorf("api token creation returned empty token")
	}
	return tokenResp.Token, nil
}

// postJSON sends a JSON POST request. If authToken is non-empty, it's sent as a Bearer token.
func postJSON(ctx context.Context, url, authToken string, body, result any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	if result != nil {
		return json.Unmarshal(data, result)
	}
	return nil
}

func setupMCP(ctx context.Context, vikunjaBaseURL, token string) (*mcp.ClientSession, func(), error) {
	apiClient := NewClient(vikunjaBaseURL, token)
	server := mcp.NewServer(&mcp.Implementation{Name: "vikunja-mcp", Version: "test"}, nil)
	registerProjectTools(server, apiClient)
	registerTaskTools(server, apiClient)
	registerLabelTools(server, apiClient)
	registerCommentTools(server, apiClient)
	registerAssigneeTools(server, apiClient)
	registerTimeEntryTools(server, apiClient)
	registerResources(server, apiClient)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	srvCtx, srvCancel := context.WithCancel(ctx)
	srvDone := make(chan error, 1)
	go func() {
		srvDone <- server.Run(srvCtx, serverTransport)
	}()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "v0.1.0"}, nil)
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		srvCancel()
		return nil, nil, fmt.Errorf("connecting MCP client: %w", err)
	}

	cleanup := func() {
		_ = session.Close()
		srvCancel()
		<-srvDone
	}
	return session, cleanup, nil
}

// toolText extracts the text string from an MCP tool result.
func toolText(t *testing.T, name string, result *mcp.CallToolResult) string {
	t.Helper()
	if result.IsError {
		tc, ok := result.Content[0].(*mcp.TextContent)
		if ok {
			t.Fatalf("CallTool(%s) returned error: %s", name, tc.Text)
		}
		t.Fatalf("CallTool(%s) returned error (non-text content)", name)
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s): expected *mcp.TextContent, got %T", name, result.Content[0])
	}
	return tc.Text
}

// callTool calls an MCP tool and parses the JSON response into a map.
func callTool(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := testSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	text := toolText(t, name, result)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("CallTool(%s): unmarshal: %v\nraw: %s", name, err, text)
	}
	return parsed
}

// callToolText calls an MCP tool and returns the raw text response.
func callToolText(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	result, err := testSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return toolText(t, name, result)
}

// jsonID extracts the "id" field from a parsed JSON map as int64.
func jsonID(t *testing.T, m map[string]any) int64 {
	t.Helper()
	v, ok := m["id"].(float64)
	if !ok {
		t.Fatalf("expected numeric id, got %T: %v", m["id"], m["id"])
	}
	return int64(v)
}

func TestE2E_TaskLifecycle(t *testing.T) {
	ctx := context.Background()

	// Verify expected tools are registered.
	tools, err := testSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	toolNames := make(map[string]bool)
	for _, tool := range tools.Tools {
		toolNames[tool.Name] = true
	}
	for _, name := range []string{
		"create_project", "create_task", "update_task",
		"complete_task", "delete_task", "get_task", "list_tasks",
	} {
		if !toolNames[name] {
			t.Errorf("missing expected tool %q", name)
		}
	}

	// Create project.
	project := callTool(t, "create_project", map[string]any{
		"title":       "E2E Test Project",
		"description": "Created by integration test",
	})
	projectID := jsonID(t, project)
	t.Logf("created project id=%d title=%q", projectID, project["title"])
	if projectID == 0 {
		t.Fatal("project ID is 0")
	}
	if project["title"] != "E2E Test Project" {
		t.Errorf("project title = %q, want %q", project["title"], "E2E Test Project")
	}

	// Create task in project.
	task := callTool(t, "create_task", map[string]any{
		"project_id":  projectID,
		"title":       "E2E Test Task",
		"description": "Integration test task",
		"priority":    3,
	})
	taskID := jsonID(t, task)
	t.Logf("created task id=%d title=%q priority=%v", taskID, task["title"], task["priority"])
	if taskID == 0 {
		t.Fatal("task ID is 0")
	}
	if task["title"] != "E2E Test Task" {
		t.Errorf("task title = %q, want %q", task["title"], "E2E Test Task")
	}

	// Update task title.
	updated := callTool(t, "update_task", map[string]any{
		"id":    taskID,
		"title": "E2E Updated Task",
	})
	t.Logf("updated task id=%d title=%q", taskID, updated["title"])
	if updated["title"] != "E2E Updated Task" {
		t.Errorf("updated title = %q, want %q", updated["title"], "E2E Updated Task")
	}

	// Complete task.
	completed := callTool(t, "complete_task", map[string]any{
		"id": taskID,
	})
	t.Logf("completed task id=%d done=%v", taskID, completed["done"])
	if completed["done"] != true {
		t.Errorf("done = %v, want true", completed["done"])
	}

	// Get task and verify completion persisted.
	fetched := callTool(t, "get_task", map[string]any{
		"id": taskID,
	})
	t.Logf("fetched task id=%d title=%q done=%v created=%v", taskID, fetched["title"], fetched["done"], fetched["created"])
	if fetched["done"] != true {
		t.Errorf("fetched done = %v, want true", fetched["done"])
	}
	if fetched["title"] != "E2E Updated Task" {
		t.Errorf("fetched title = %q, want %q", fetched["title"], "E2E Updated Task")
	}

	// List projects — must return items array with our project.
	projectList := callTool(t, "list_projects", nil)
	t.Logf("list_projects response: %v", projectList)
	projectItems, ok := projectList["items"].([]any)
	if !ok {
		t.Fatalf("list_projects: expected items array, got %T (full response: %v)", projectList["items"], projectList)
	}
	if len(projectItems) == 0 {
		t.Fatal("list_projects: items array is empty")
	}

	// List tasks — must return items array with our task.
	taskList := callTool(t, "list_tasks", map[string]any{
		"project_id": projectID,
	})
	t.Logf("list_tasks response: %v", taskList)
	taskItems, ok := taskList["items"].([]any)
	if !ok {
		t.Fatalf("list_tasks: expected items array, got %T (full response: %v)", taskList["items"], taskList)
	}
	if len(taskItems) == 0 {
		t.Fatal("list_tasks: items array is empty")
	}

	// Delete task.
	deleteText := callToolText(t, "delete_task", map[string]any{
		"id": taskID,
	})
	t.Logf("deleted task id=%d result=%q", taskID, deleteText)
	if deleteText != "deleted" {
		t.Errorf("delete result = %q, want %q", deleteText, "deleted")
	}
}

func TestE2E_Resources(t *testing.T) {
	ctx := context.Background()

	// Create test data.
	project := callTool(t, "create_project", map[string]any{
		"title": "Resource Test Project",
	})
	projectID := jsonID(t, project)

	task := callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Resource Test Task",
	})
	taskID := jsonID(t, task)

	// Read vikunja://projects — list all projects.
	result, err := testSession.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "vikunja://projects",
	})
	if err != nil {
		t.Fatalf("ReadResource(projects): %v", err)
	}
	if len(result.Contents) == 0 || result.Contents[0].Text == "" {
		t.Fatal("empty response for vikunja://projects")
	}

	// Read vikunja://projects/{id} — single project.
	result, err = testSession.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: fmt.Sprintf("vikunja://projects/%d", projectID),
	})
	if err != nil {
		t.Fatalf("ReadResource(project %d): %v", projectID, err)
	}
	var proj map[string]any
	unmarshalErr := json.Unmarshal([]byte(result.Contents[0].Text), &proj)
	if unmarshalErr != nil {
		t.Fatalf("unmarshal project resource: %v", unmarshalErr)
	}
	if proj["title"] != "Resource Test Project" {
		t.Errorf("project title = %v, want %q", proj["title"], "Resource Test Project")
	}

	// Read vikunja://projects/{id}/tasks — project tasks.
	result, err = testSession.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: fmt.Sprintf("vikunja://projects/%d/tasks", projectID),
	})
	if err != nil {
		t.Fatalf("ReadResource(project %d tasks): %v", projectID, err)
	}
	if len(result.Contents) == 0 || result.Contents[0].Text == "" {
		t.Fatal("empty response for project tasks resource")
	}

	// Read vikunja://tasks/{id} — single task.
	result, err = testSession.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: fmt.Sprintf("vikunja://tasks/%d", taskID),
	})
	if err != nil {
		t.Fatalf("ReadResource(task %d): %v", taskID, err)
	}
	var taskRes map[string]any
	unmarshalErr = json.Unmarshal([]byte(result.Contents[0].Text), &taskRes)
	if unmarshalErr != nil {
		t.Fatalf("unmarshal task resource: %v", unmarshalErr)
	}
	if taskRes["title"] != "Resource Test Task" {
		t.Errorf("task title = %v, want %q", taskRes["title"], "Resource Test Task")
	}
}
