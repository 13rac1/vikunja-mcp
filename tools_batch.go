package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type BatchCreateTasksInput struct {
	ProjectID int64            `json:"project_id"  jsonschema:"project to create tasks in,required"`
	Tasks     []map[string]any `json:"tasks"       jsonschema:"array of task objects (each must have at least title),required"`
}

type BatchUpdateTasksInput struct {
	Tasks []map[string]any `json:"tasks" jsonschema:"array of task objects (each must have id plus fields to update),required"`
}

// batchErrJSON returns a JSON-encoded error object for batch result arrays.
func batchErrJSON(msg string) json.RawMessage {
	b, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		return json.RawMessage(fmt.Sprintf(`{"error":%q}`, msg))
	}
	return b
}

func registerBatchTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "batch_create_tasks",
		Description: "Create multiple tasks in a single project. Each task object must have at least a title. Returns an array of results (created task or error per item).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input BatchCreateTasksInput) (*mcp.CallToolResult, any, error) {
		path := fmt.Sprintf("/projects/%d/tasks", input.ProjectID)
		results := make([]json.RawMessage, 0, len(input.Tasks))
		failures := 0
		for _, task := range input.Tasks {
			raw, err := client.doRaw(ctx, "POST", path, task)
			if err != nil {
				results = append(results, batchErrJSON(err.Error()))
				failures++
				continue
			}
			filtered, filterErr := filterJSON(raw, taskFields)
			if filterErr != nil {
				filtered = raw
			}
			results = append(results, filtered)
		}
		out, err := json.Marshal(results)
		if err != nil {
			return errorResult(err), nil, nil
		}
		r := textResult(out)
		if failures == len(input.Tasks) {
			r.IsError = true
		}
		return r, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "batch_update_tasks",
		Description: "Update multiple tasks at once. Each object must have an id field plus the fields to update. Returns an array of results (updated task or error per item).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input BatchUpdateTasksInput) (*mcp.CallToolResult, any, error) {
		results := make([]json.RawMessage, 0, len(input.Tasks))
		failures := 0
		for _, task := range input.Tasks {
			idVal, ok := task["id"]
			if !ok {
				results = append(results, batchErrJSON("missing id field"))
				failures++
				continue
			}
			idFloat, ok := idVal.(float64)
			if !ok {
				results = append(results, batchErrJSON(fmt.Sprintf("id must be a number, got %T", idVal)))
				failures++
				continue
			}
			raw, err := client.doRaw(ctx, "PATCH", fmt.Sprintf("/tasks/%d", int64(idFloat)), task)
			if err != nil {
				results = append(results, batchErrJSON(err.Error()))
				failures++
				continue
			}
			filtered, filterErr := filterJSON(raw, taskFields)
			if filterErr != nil {
				filtered = raw
			}
			results = append(results, filtered)
		}
		out, err := json.Marshal(results)
		if err != nil {
			return errorResult(err), nil, nil
		}
		r := textResult(out)
		if failures == len(input.Tasks) {
			r.IsError = true
		}
		return r, nil, nil
	})
}
