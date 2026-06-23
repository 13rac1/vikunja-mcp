package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListTasksInput struct {
	ProjectID          int64    `json:"project_id,omitempty"           jsonschema:"filter to tasks in this project"`
	Filter             string   `json:"filter,omitempty"               jsonschema:"Vikunja filter query (see vikunja.io/docs/filters)"`
	Search             string   `json:"search,omitempty"               jsonschema:"search string to match against task titles"`
	FilterTimezone     string   `json:"filter_timezone,omitempty"      jsonschema:"timezone for relative date filters like now"`
	FilterIncludeNulls bool     `json:"filter_include_nulls,omitempty" jsonschema:"include tasks where filtered field is null"`
	SortBy             []string `json:"sort_by,omitempty"              jsonschema:"fields to sort by (e.g. done, priority, due_date)"`
	OrderBy            []string `json:"order_by,omitempty"             jsonschema:"sort direction per sort_by field: asc or desc"`
	Page               int      `json:"page,omitempty"                 jsonschema:"page number (1-based, default 1)"`
	PerPage            int      `json:"per_page,omitempty"             jsonschema:"items per page (default 50, max 1000)"`
}

type GetTaskInput struct {
	ID int64 `json:"id" jsonschema:"the numeric task ID,required"`
}

type CreateTaskInput struct {
	ProjectID   int64  `json:"project_id"                  jsonschema:"project to create the task in,required"`
	Title       string `json:"title"                       jsonschema:"task title,required"`
	Description string `json:"description,omitempty"       jsonschema:"task description (HTML)"`
	Priority    int64  `json:"priority,omitempty"          jsonschema:"priority (higher number = more important)"`
	DueDate     string `json:"due_date,omitempty"          jsonschema:"due date in ISO 8601 format"`
	StartDate   string `json:"start_date,omitempty"        jsonschema:"start date in ISO 8601 format"`
	EndDate     string `json:"end_date,omitempty"          jsonschema:"end date in ISO 8601 format"`
	HexColor    string `json:"hex_color,omitempty"         jsonschema:"hex color without leading #"`
	RepeatAfter int64  `json:"repeat_after,omitempty"      jsonschema:"repeat interval in seconds"`
}

type UpdateTaskInput struct {
	ID          int64   `json:"id"                          jsonschema:"task ID,required"`
	Title       *string `json:"title,omitempty"             jsonschema:"new title"`
	Description *string `json:"description,omitempty"       jsonschema:"new description"`
	Done        *bool   `json:"done,omitempty"              jsonschema:"completion status"`
	Priority    *int64  `json:"priority,omitempty"          jsonschema:"new priority"`
	DueDate     *string `json:"due_date,omitempty"          jsonschema:"new due date (ISO 8601)"`
	StartDate   *string `json:"start_date,omitempty"        jsonschema:"new start date (ISO 8601)"`
	EndDate     *string `json:"end_date,omitempty"          jsonschema:"new end date (ISO 8601)"`
	HexColor    *string `json:"hex_color,omitempty"         jsonschema:"new hex color without #"`
	ProjectID   *int64  `json:"project_id,omitempty"        jsonschema:"move task to a different project"`
}

type DeleteTaskInput struct {
	ID int64 `json:"id" jsonschema:"the numeric task ID to delete,required"`
}

type CompleteTaskInput struct {
	ID int64 `json:"id" jsonschema:"the numeric task ID to mark as done,required"`
}

type SearchTasksInput struct {
	Query              string   `json:"query"                          jsonschema:"search query string,required"`
	Filter             string   `json:"filter,omitempty"               jsonschema:"additional Vikunja filter query"`
	FilterTimezone     string   `json:"filter_timezone,omitempty"      jsonschema:"timezone for relative date filters"`
	FilterIncludeNulls bool     `json:"filter_include_nulls,omitempty" jsonschema:"include tasks where filtered field is null"`
	SortBy             []string `json:"sort_by,omitempty"              jsonschema:"fields to sort by"`
	OrderBy            []string `json:"order_by,omitempty"             jsonschema:"sort direction per sort_by: asc or desc"`
	Page               int      `json:"page,omitempty"                 jsonschema:"page number"`
	PerPage            int      `json:"per_page,omitempty"             jsonschema:"items per page"`
}

func buildTaskListQuery(search, filter, filterTimezone string, filterIncludeNulls bool, sortBy, orderBy []string, page, perPage int) string {
	params := buildPageQuery(search, page, perPage)
	if filter != "" {
		params.Set("filter", filter)
	}
	if filterTimezone != "" {
		params.Set("filter_timezone", filterTimezone)
	}
	if filterIncludeNulls {
		params.Set("filter_include_nulls", "true")
	}
	for _, s := range sortBy {
		params.Add("sort_by", s)
	}
	for _, o := range orderBy {
		params.Add("order_by", o)
	}
	return appendQuery("", params)
}

func registerTaskTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List tasks. Returns all tasks across projects, or tasks in a specific project if project_id is given. Supports filtering, sorting, searching, and pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListTasksInput) (*mcp.CallToolResult, any, error) {
		var basePath string
		if input.ProjectID > 0 {
			basePath = fmt.Sprintf("/projects/%d/tasks", input.ProjectID)
		} else {
			basePath = "/tasks"
		}
		query := buildTaskListQuery(input.Search, input.Filter, input.FilterTimezone, input.FilterIncludeNulls, input.SortBy, input.OrderBy, input.Page, input.PerPage)
		raw, err := client.doRaw(ctx, "GET", basePath+query, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task",
		Description: "Get a single task by its numeric ID. Returns the full task object including assignees, labels, and related tasks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetTaskInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "GET", fmt.Sprintf("/tasks/%d", input.ID), nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task in a project. Requires project_id and title.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateTaskInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "POST", fmt.Sprintf("/projects/%d/tasks", input.ProjectID), input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_task",
		Description: "Update a task's fields. Only the provided fields are changed (partial update via PATCH). Can also move a task to a different project by setting project_id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateTaskInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "PATCH", fmt.Sprintf("/tasks/%d", input.ID), input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete a task by its numeric ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteTaskInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/tasks/%d", input.ID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "complete_task",
		Description: "Mark a task as done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CompleteTaskInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "PATCH", fmt.Sprintf("/tasks/%d", input.ID), map[string]any{"done": true})
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_tasks",
		Description: "Search for tasks by a query string. Optionally combine with a Vikunja filter expression for more precise results.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchTasksInput) (*mcp.CallToolResult, any, error) {
		query := buildTaskListQuery(input.Query, input.Filter, input.FilterTimezone, input.FilterIncludeNulls, input.SortBy, input.OrderBy, input.Page, input.PerPage)
		raw, err := client.doRaw(ctx, "GET", "/tasks"+query, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})
}
