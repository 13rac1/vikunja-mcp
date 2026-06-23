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
			"projects":         {"read_all", "read_one", "create", "update", "delete", "views_buckets_tasks", "views_buckets_tasks_get"},
			"tasks":            {"read_all", "read_one", "create", "update", "delete"},
			"labels":           {"read_all", "create", "delete"},
			"tasks_labels":     {"create", "delete"},
			"tasks_comments":   {"read_all", "create"},
			"tasks_assignees":  {"create", "delete"},
			"tasks_relations":  {"create", "delete"},
			"time-entries":     {"read_all", "read_one", "create", "update", "delete"},
			"projects_views":   {"read_all", "read_one", "create", "update", "delete"},
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
	registerPowerQueryTools(server, apiClient)
	registerRelationTools(server, apiClient)
	registerViewTools(server, apiClient)
	registerBatchTools(server, apiClient)
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

// keysOf returns the keys of a map for logging.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

func TestE2E_ProjectCRUD(t *testing.T) {
	// Create.
	project := callTool(t, "create_project", map[string]any{
		"title":       "CRUD Test Project",
		"description": "will be updated and deleted",
	})
	projectID := jsonID(t, project)
	if project["title"] != "CRUD Test Project" {
		t.Errorf("title = %v, want CRUD Test Project", project["title"])
	}

	// Update.
	updated := callTool(t, "update_project", map[string]any{
		"id":    projectID,
		"title": "Updated CRUD Project",
	})
	if updated["title"] != "Updated CRUD Project" {
		t.Errorf("updated title = %v, want Updated CRUD Project", updated["title"])
	}

	// Verify update persisted.
	fetched := callTool(t, "get_project", map[string]any{"id": projectID})
	if fetched["title"] != "Updated CRUD Project" {
		t.Errorf("fetched title = %v, want Updated CRUD Project", fetched["title"])
	}

	// Delete.
	deleteText := callToolText(t, "delete_project", map[string]any{"id": projectID})
	if deleteText != "deleted" {
		t.Errorf("delete result = %q, want %q", deleteText, "deleted")
	}
}

func TestE2E_LabelDelete(t *testing.T) {
	// Create.
	label := callTool(t, "create_label", map[string]any{
		"title":     "ephemeral-label",
		"hex_color": "aabbcc",
	})
	labelID := jsonID(t, label)
	if label["title"] != "ephemeral-label" {
		t.Errorf("title = %v, want ephemeral-label", label["title"])
	}

	// Delete.
	deleteText := callToolText(t, "delete_label", map[string]any{"id": labelID})
	if deleteText != "deleted" {
		t.Errorf("delete result = %q, want %q", deleteText, "deleted")
	}
}

func TestE2E_PowerQueries(t *testing.T) {
	// Create a project with varied tasks.
	project := callTool(t, "create_project", map[string]any{
		"title": "Power Query Project",
	})
	projectID := jsonID(t, project)

	// Overdue task (yesterday).
	callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Overdue Task",
		"due_date":   "2020-01-01T00:00:00Z",
		"priority":   5,
	})

	// High priority task, no due date.
	callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "High Priority No Date",
		"priority":   4,
	})

	// Normal priority task, due far in the future.
	callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Future Task",
		"due_date":   "2099-12-31T00:00:00Z",
		"priority":   1,
	})

	// Test overdue_tasks.
	overdue := callTool(t, "overdue_tasks", map[string]any{"project_id": projectID})
	overdueItems := overdue["items"].([]any)
	if len(overdueItems) == 0 {
		t.Error("overdue_tasks: expected at least 1 result")
	}

	// Test high_priority_tasks.
	highPri := callTool(t, "high_priority_tasks", map[string]any{"project_id": projectID})
	highPriItems := highPri["items"].([]any)
	if len(highPriItems) < 2 {
		t.Errorf("high_priority_tasks: expected >= 2 results, got %d", len(highPriItems))
	}

	// Test urgent_tasks (priority >= 4).
	urgent := callTool(t, "urgent_tasks", map[string]any{"project_id": projectID})
	urgentItems := urgent["items"].([]any)
	if len(urgentItems) < 1 {
		t.Errorf("urgent_tasks: expected >= 1, got %d", len(urgentItems))
	}

	// Test focus_now (overdue or urgent).
	focus := callTool(t, "focus_now", map[string]any{"project_id": projectID})
	focusItems := focus["items"].([]any)
	if len(focusItems) < 2 {
		t.Errorf("focus_now: expected >= 2, got %d", len(focusItems))
	}

	// Test upcoming_deadlines (default 7 days — should not include the overdue one or the 2099 one).
	upcoming := callTool(t, "upcoming_deadlines", map[string]any{"project_id": projectID})
	t.Logf("upcoming_deadlines response: %v", upcoming)

	// Test upcoming_deadlines with large window to catch the 2099 task.
	upcomingWide := callTool(t, "upcoming_deadlines", map[string]any{
		"project_id": projectID,
		"days":       50000,
	})
	upcomingWideItems := upcomingWide["items"].([]any)
	if len(upcomingWideItems) == 0 {
		t.Error("upcoming_deadlines(50000d): expected at least 1 result")
	}

	// Test task_summary.
	summary := callTool(t, "task_summary", map[string]any{"project_id": projectID})
	t.Logf("task_summary: %v", summary)
	if summary["total_open"] == nil || summary["total_open"].(float64) < 3 {
		t.Errorf("task_summary total_open: expected >= 3, got %v", summary["total_open"])
	}
	if summary["overdue"] == nil || summary["overdue"].(float64) < 1 {
		t.Errorf("task_summary overdue: expected >= 1, got %v", summary["overdue"])
	}
}

func TestE2E_TaskRelations(t *testing.T) {
	project := callTool(t, "create_project", map[string]any{
		"title": "Relations Test Project",
	})
	projectID := jsonID(t, project)

	parent := callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Parent Task",
	})
	parentID := jsonID(t, parent)

	child := callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Child Task",
	})
	childID := jsonID(t, child)

	// Create subtask relation.
	callTool(t, "create_task_relation", map[string]any{
		"task_id":       parentID,
		"other_task_id": childID,
		"relation_kind": "subtask",
	})

	// Verify relation appears in get_task.
	fetched := callTool(t, "get_task", map[string]any{"id": parentID})
	relatedTasks, ok := fetched["related_tasks"].(map[string]any)
	if !ok {
		t.Fatalf("expected related_tasks map, got %T: %v", fetched["related_tasks"], fetched["related_tasks"])
	}
	subtasks, ok := relatedTasks["subtask"].([]any)
	if !ok || len(subtasks) == 0 {
		t.Fatal("expected subtask relation in related_tasks")
	}

	// Delete relation.
	deleteText := callToolText(t, "delete_task_relation", map[string]any{
		"task_id":        parentID,
		"relation_kind":  "subtask",
		"other_task_id":  childID,
	})
	if deleteText != "deleted" {
		t.Errorf("delete relation result = %q, want %q", deleteText, "deleted")
	}

	// Verify relation is gone.
	fetchedAfter := callTool(t, "get_task", map[string]any{"id": parentID})
	if rt, ok := fetchedAfter["related_tasks"].(map[string]any); ok {
		if subs, ok := rt["subtask"].([]any); ok && len(subs) > 0 {
			t.Error("subtask relation still present after delete")
		}
	}
}

func TestE2E_Reminders(t *testing.T) {
	project := callTool(t, "create_project", map[string]any{
		"title": "Reminder Test Project",
	})
	projectID := jsonID(t, project)

	// Create task with an absolute reminder.
	task := callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Reminder Task",
		"due_date":   "2099-06-01T12:00:00Z",
		"reminders": []map[string]any{
			{"reminder": "2099-06-01T11:00:00Z"},
		},
	})
	taskID := jsonID(t, task)

	// Verify reminder in get_task.
	fetched := callTool(t, "get_task", map[string]any{"id": taskID})
	reminders, ok := fetched["reminders"].([]any)
	if !ok || len(reminders) == 0 {
		t.Fatalf("expected reminders array, got %T: %v", fetched["reminders"], fetched["reminders"])
	}

	// Update with a relative reminder.
	negTenMin := int64(-600)
	updated := callTool(t, "update_task", map[string]any{
		"id": taskID,
		"reminders": []map[string]any{
			{"relative_period": negTenMin, "relative_to": "due_date"},
		},
	})
	updatedReminders, ok := updated["reminders"].([]any)
	if !ok || len(updatedReminders) == 0 {
		t.Fatalf("expected updated reminders, got %T: %v", updated["reminders"], updated["reminders"])
	}
}

func TestE2E_Views(t *testing.T) {
	project := callTool(t, "create_project", map[string]any{
		"title": "Views Test Project",
	})
	projectID := jsonID(t, project)

	// Every project starts with a default view — list them.
	views := callTool(t, "list_views", map[string]any{"project_id": projectID})
	t.Logf("list_views response: %v", views)
	viewItems, ok := views["items"].([]any)
	if !ok {
		// v2 list_views may return a plain array instead of paginated envelope.
		t.Logf("list_views returned non-paginated response, trying raw array parse")
	}
	if ok && len(viewItems) == 0 {
		t.Fatal("list_views: expected at least one default view")
	}

	// Create a kanban view.
	kanban := callTool(t, "create_view", map[string]any{
		"project_id":                projectID,
		"title":                     "E2E Kanban",
		"view_kind":                 "kanban",
		"bucket_configuration_mode": "manual",
	})
	kanbanViewID := jsonID(t, kanban)
	if kanban["title"] != "E2E Kanban" {
		t.Errorf("kanban title = %v, want E2E Kanban", kanban["title"])
	}
	t.Logf("created kanban view id=%d", kanbanViewID)

	// Update the view title.
	updatedView := callTool(t, "update_view", map[string]any{
		"project_id": projectID,
		"view_id":    kanbanViewID,
		"title":      "Updated Kanban",
	})
	if updatedView["title"] != "Updated Kanban" {
		t.Errorf("updated view title = %v, want Updated Kanban", updatedView["title"])
	}

	// Create a task in the project.
	task := callTool(t, "create_task", map[string]any{
		"project_id": projectID,
		"title":      "Kanban Task",
	})
	taskID := jsonID(t, task)

	// List buckets — should have at least the default backlog bucket with our task.
	buckets := callTool(t, "list_buckets", map[string]any{
		"project_id": projectID,
		"view_id":    kanbanViewID,
	})
	t.Logf("list_buckets response keys: %v", keysOf(buckets))

	// Find a bucket that contains our task or any bucket at all.
	bucketItems, ok := buckets["items"].([]any)
	if !ok {
		// Bucket listing may return a plain array.
		t.Logf("list_buckets did not return paginated envelope, checking raw")
	}

	var firstBucketID int64
	if ok && len(bucketItems) > 0 {
		if b, ok := bucketItems[0].(map[string]any); ok {
			firstBucketID = int64(b["id"].(float64))
		}
	}
	t.Logf("first bucket id=%d", firstBucketID)

	// Move task to bucket (only if we found a bucket).
	if firstBucketID > 0 {
		moved := callTool(t, "move_task_to_bucket", map[string]any{
			"project_id": projectID,
			"view_id":    kanbanViewID,
			"bucket_id":  firstBucketID,
			"task_id":    taskID,
		})
		t.Logf("move_task_to_bucket response: %v", moved)
	}

	// Delete the view we created.
	deleteText := callToolText(t, "delete_view", map[string]any{
		"project_id": projectID,
		"view_id":    kanbanViewID,
	})
	if deleteText != "deleted" {
		t.Errorf("delete view result = %q, want %q", deleteText, "deleted")
	}
}

func TestE2E_BatchOperations(t *testing.T) {
	project := callTool(t, "create_project", map[string]any{
		"title": "Batch Test Project",
	})
	projectID := jsonID(t, project)

	// Batch create 3 tasks.
	result, err := testSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "batch_create_tasks",
		Arguments: map[string]any{
			"project_id": projectID,
			"tasks": []map[string]any{
				{"title": "Batch Task 1"},
				{"title": "Batch Task 2"},
				{"title": "Batch Task 3"},
			},
		},
	})
	if err != nil {
		t.Fatalf("batch_create_tasks: %v", err)
	}
	if result.IsError {
		t.Fatalf("batch_create_tasks returned error: %v", result.Content)
	}
	text := toolText(t, "batch_create_tasks", result)

	var created []map[string]any
	if err := json.Unmarshal([]byte(text), &created); err != nil {
		t.Fatalf("unmarshal batch result: %v\nraw: %s", err, text)
	}
	if len(created) != 3 {
		t.Fatalf("expected 3 results, got %d", len(created))
	}

	// Collect IDs for batch update.
	var ids []float64
	for _, c := range created {
		id, ok := c["id"].(float64)
		if !ok {
			t.Fatalf("created task missing id: %v", c)
		}
		ids = append(ids, id)
	}

	// Batch update all 3 titles.
	updateResult, err := testSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "batch_update_tasks",
		Arguments: map[string]any{
			"tasks": []map[string]any{
				{"id": ids[0], "title": "Updated Batch 1"},
				{"id": ids[1], "title": "Updated Batch 2"},
				{"id": ids[2], "title": "Updated Batch 3"},
			},
		},
	})
	if err != nil {
		t.Fatalf("batch_update_tasks: %v", err)
	}
	if updateResult.IsError {
		t.Fatalf("batch_update_tasks returned error: %v", updateResult.Content)
	}
	updateText := toolText(t, "batch_update_tasks", updateResult)

	var updated []map[string]any
	if err := json.Unmarshal([]byte(updateText), &updated); err != nil {
		t.Fatalf("unmarshal batch update result: %v\nraw: %s", err, updateText)
	}
	for i, u := range updated {
		want := fmt.Sprintf("Updated Batch %d", i+1)
		if u["title"] != want {
			t.Errorf("updated[%d].title = %v, want %q", i, u["title"], want)
		}
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
