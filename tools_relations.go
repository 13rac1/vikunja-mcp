package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateTaskRelationInput struct {
	TaskID       int64  `json:"task_id"        jsonschema:"the task ID,required"`
	OtherTaskID  int64  `json:"other_task_id"  jsonschema:"the related task ID,required"`
	RelationKind string `json:"relation_kind"  jsonschema:"relation type: subtask parenttask related blocking blocked precedes follows duplicateof duplicates copiedfrom copiedto,required"`
}

type DeleteTaskRelationInput struct {
	TaskID       int64  `json:"task_id"        jsonschema:"the task ID,required"`
	RelationKind string `json:"relation_kind"  jsonschema:"the relation type to remove,required"`
	OtherTaskID  int64  `json:"other_task_id"  jsonschema:"the related task ID to unlink,required"`
}

func registerRelationTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task_relation",
		Description: "Create a relation between two tasks (e.g. subtask, blocking, related).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateTaskRelationInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"other_task_id": input.OtherTaskID,
			"relation_kind": input.RelationKind,
		}
		raw, err := client.doRaw(ctx, "POST", fmt.Sprintf("/tasks/%d/relations", input.TaskID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskRelationFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_task_relation",
		Description: "Remove a relation between two tasks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteTaskRelationInput) (*mcp.CallToolResult, any, error) {
		path := fmt.Sprintf("/tasks/%d/relations/%s/%d", input.TaskID, input.RelationKind, input.OtherTaskID)
		err := client.do(ctx, "DELETE", path, nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})
}
