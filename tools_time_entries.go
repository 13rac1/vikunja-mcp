package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListTimeEntriesInput struct {
	Filter         string `json:"filter,omitempty"          jsonschema:"filter query over user_id, task_id, project_id, start_time, end_time (e.g. project_id = 5 && start_time > now-7d)"`
	FilterTimezone string `json:"filter_timezone,omitempty" jsonschema:"IANA timezone for relative dates in filter (e.g. Europe/Berlin)"`
	Page           int    `json:"page,omitempty"            jsonschema:"page number (1-based, default 1)"`
	PerPage        int    `json:"per_page,omitempty"        jsonschema:"items per page (default 50, max 1000)"`
	Search         string `json:"search,omitempty"          jsonschema:"search string"`
}

type CreateTimeEntryInput struct {
	TaskID    *int64 `json:"task_id,omitempty"    jsonschema:"task to log time against (exactly one of task_id or project_id)"`
	ProjectID *int64 `json:"project_id,omitempty" jsonschema:"project to log time against (exactly one of task_id or project_id)"`
	StartTime string `json:"start_time"           jsonschema:"start time in ISO 8601 format,required"`
	EndTime   string `json:"end_time,omitempty"   jsonschema:"end time in ISO 8601 (omit to start a live timer)"`
	Comment   string `json:"comment,omitempty"    jsonschema:"description of the logged time"`
}

type UpdateTimeEntryInput struct {
	ID        int64   `json:"id"                   jsonschema:"time entry ID,required"`
	TaskID    *int64  `json:"task_id,omitempty"    jsonschema:"move to a different task"`
	ProjectID *int64  `json:"project_id,omitempty" jsonschema:"move to a different project"`
	StartTime *string `json:"start_time,omitempty" jsonschema:"new start time (ISO 8601)"`
	EndTime   *string `json:"end_time,omitempty"   jsonschema:"new end time (ISO 8601)"`
	Comment   *string `json:"comment,omitempty"    jsonschema:"new comment"`
}

type DeleteTimeEntryInput struct {
	ID int64 `json:"id" jsonschema:"time entry ID to delete,required"`
}

func registerTimeEntryTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_time_entries",
		Description: "List time entries. Supports filtering by date range, project, task, and user. Requires the time tracking license feature to be enabled on the server.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListTimeEntriesInput) (*mcp.CallToolResult, any, error) {
		params := buildPageQuery(input.Search, input.Page, input.PerPage)
		if input.Filter != "" {
			params.Set("filter", input.Filter)
		}
		if input.FilterTimezone != "" {
			params.Set("filter_timezone", input.FilterTimezone)
		}
		path := appendQuery("/time-entries", params)
		raw, err := client.doRaw(ctx, "GET", path, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, timeEntryFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_time_entry",
		Description: "Create a time entry. Exactly one of task_id or project_id must be set. Omit end_time to start a live timer. Requires the time tracking license feature.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateTimeEntryInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "POST", "/time-entries", input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, timeEntryFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_time_entry",
		Description: "Update a time entry. Only the provided fields are changed (partial update via PATCH). Requires the time tracking license feature.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateTimeEntryInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "PATCH", fmt.Sprintf("/time-entries/%d", input.ID), input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, timeEntryFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_time_entry",
		Description: "Delete a time entry. If it is the running timer, this also stops it. Requires the time tracking license feature.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteTimeEntryInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/time-entries/%d", input.ID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_timer",
		Description: "Stop the authenticated user's currently running timer. Returns the stopped time entry. Returns an error if no timer is running. Requires the time tracking license feature.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "POST", "/time-entries/timer/stop", nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, timeEntryFields), nil, nil
	})
}
