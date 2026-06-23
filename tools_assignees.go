package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AddAssigneeInput struct {
	TaskID int64 `json:"task_id" jsonschema:"the task ID,required"`
	UserID int64 `json:"user_id" jsonschema:"the user ID to assign,required"`
}

type RemoveAssigneeInput struct {
	TaskID int64 `json:"task_id" jsonschema:"the task ID,required"`
	UserID int64 `json:"user_id" jsonschema:"the user ID to unassign,required"`
}

func registerAssigneeTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_assignee",
		Description: "Assign a user to a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddAssigneeInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"user_id": input.UserID,
		}
		raw, err := client.doRaw(ctx, "POST", fmt.Sprintf("/tasks/%d/assignees", input.TaskID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_assignee",
		Description: "Remove a user assignment from a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RemoveAssigneeInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/tasks/%d/assignees/%d", input.TaskID, input.UserID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})
}
