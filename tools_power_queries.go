package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PowerQueryInput struct {
	ProjectID int64 `json:"project_id,omitempty" jsonschema:"scope to this project (omit for all projects)"`
	Page      int   `json:"page,omitempty"       jsonschema:"page number (1-based, default 1)"`
	PerPage   int   `json:"per_page,omitempty"   jsonschema:"items per page (default 50, max 1000)"`
}

type UpcomingDeadlinesInput struct {
	ProjectID int64 `json:"project_id,omitempty" jsonschema:"scope to this project (omit for all projects)"`
	Days      int   `json:"days,omitempty"       jsonschema:"number of days ahead to look (default 7)"`
	Page      int   `json:"page,omitempty"       jsonschema:"page number (1-based, default 1)"`
	PerPage   int   `json:"per_page,omitempty"   jsonschema:"items per page (default 50, max 1000)"`
}

type TaskSummaryInput struct {
	ProjectID int64 `json:"project_id,omitempty" jsonschema:"scope to this project (omit for all projects)"`
}

// powerQuery runs a pre-baked filter query and returns filtered task results.
func powerQuery(ctx context.Context, client *Client, filter string, includeNulls bool, projectID int64, page, perPage int) *mcp.CallToolResult {
	var basePath string
	if projectID > 0 {
		basePath = fmt.Sprintf("/projects/%d/tasks", projectID)
	} else {
		basePath = "/tasks"
	}
	query := buildTaskListQuery("", filter, "", includeNulls, nil, nil, page, perPage)
	raw, err := client.doRaw(ctx, "GET", basePath+query, nil)
	if err != nil {
		return errorResult(err)
	}
	return filteredResult(raw, taskFields)
}

func registerPowerQueryTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "overdue_tasks",
		Description: "List tasks that are past their due date and not yet done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "due_date < 'now' && done = false", false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "due_today",
		Description: "List tasks due today that are not yet done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "due_date >= 'now/d' && due_date < 'now/d+1d' && done = false", false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "due_this_week",
		Description: "List tasks due within the next 7 days that are not yet done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "due_date >= 'now/d' && due_date < 'now+7d' && done = false", false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "high_priority_tasks",
		Description: "List open tasks with priority 3 (high) or above.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "priority >= 3 && done = false", false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "urgent_tasks",
		Description: "List open tasks with priority 4 (urgent) or above.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "priority >= 4 && done = false", false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "focus_now",
		Description: "List tasks that need immediate attention: either urgent (priority >= 4) or overdue, and not done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "(priority >= 4 || due_date < 'now') && done = false", false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "unscheduled_tasks",
		Description: "List open tasks that have no due date set.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PowerQueryInput) (*mcp.CallToolResult, any, error) {
		return powerQuery(ctx, client, "done = false", true, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "upcoming_deadlines",
		Description: "List open tasks with a due date within the next N days (default 7).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpcomingDeadlinesInput) (*mcp.CallToolResult, any, error) {
		days := input.Days
		if days <= 0 {
			days = 7
		}
		filter := fmt.Sprintf("due_date > 'now' && due_date < 'now+%dd' && done = false", days)
		return powerQuery(ctx, client, filter, false, input.ProjectID, input.Page, input.PerPage), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "task_summary",
		Description: "Get a quick count summary of tasks: overdue, due today, high priority, and total open. The most token-efficient way to get a project overview.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input TaskSummaryInput) (*mcp.CallToolResult, any, error) {
		var basePath string
		if input.ProjectID > 0 {
			basePath = fmt.Sprintf("/projects/%d/tasks", input.ProjectID)
		} else {
			basePath = "/tasks"
		}

		type countQuery struct {
			name   string
			filter string
		}
		queries := []countQuery{
			{"overdue", "due_date < 'now' && done = false"},
			{"due_today", "due_date >= 'now/d' && due_date < 'now/d+1d' && done = false"},
			{"high_priority", "priority >= 3 && done = false"},
			{"total_open", "done = false"},
		}

		summary := make(map[string]int64)
		for _, q := range queries {
			query := buildTaskListQuery("", q.filter, "", false, nil, nil, 1, 1)
			raw, err := client.doRaw(ctx, "GET", basePath+query, nil)
			if err != nil {
				return errorResult(err), nil, nil
			}
			var envelope struct {
				Total int64 `json:"total"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				return errorResult(fmt.Errorf("parsing %s count: %w", q.name, err)), nil, nil
			}
			summary[q.name] = envelope.Total
		}

		out, err := json.Marshal(summary)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(out), nil, nil
	})
}
