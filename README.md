# vikunja-mcp

An [MCP](https://modelcontextprotocol.io/) server that wraps the [Vikunja](https://vikunja.io/) v2 REST API, enabling AI agents to manage tasks, projects, labels, and more.

## Installation

```bash
go install github.com/13rac1/vikunja-mcp@latest
```

Or build from source:

```bash
git clone https://github.com/13rac1/vikunja-mcp.git
cd vikunja-mcp
go build -o vikunja-mcp .
```

## Configuration

Set two environment variables:

| Variable | Description |
|----------|-------------|
| `VIKUNJA_URL` | Base URL of your Vikunja instance (e.g. `https://vikunja.example.com`) |
| `VIKUNJA_TOKEN` | API token (starts with `tk_`). See [Creating an API Token](#creating-an-api-token). |

### Creating an API Token

1. Log in to your Vikunja instance and go to **Settings > API Tokens**, or use the API directly:

```bash
# Log in to get a JWT
JWT=$(curl -s -X POST https://vikunja.example.com/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username": "your_user", "password": "your_pass"}' | jq -r .token)

# Create a scoped API token
curl -s -X POST https://vikunja.example.com/api/v2/tokens \
  -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "vikunja-mcp",
    "expires_at": "2099-01-01T00:00:00Z",
    "permissions": {
      "projects":        ["read_all", "read_one", "create", "update", "delete", "views_buckets_tasks", "views_buckets_tasks_get"],
      "tasks":           ["read_all", "read_one", "create", "update", "delete"],
      "labels":          ["read_all", "create", "delete"],
      "tasks_labels":    ["create", "delete"],
      "tasks_comments":  ["read_all", "create"],
      "tasks_assignees": ["create", "delete"],
      "tasks_relations": ["create", "delete"],
      "projects_views":  ["read_all", "read_one", "create", "update", "delete"],
      "other":           ["user"]
    }
  }' | jq .token
```

2. The response contains the `tk_...` token **once** — it cannot be retrieved again. Set it as `VIKUNJA_TOKEN`.

## Usage

### Claude Desktop / Claude Code

Add to your MCP configuration:

```json
{
  "mcpServers": {
    "vikunja": {
      "command": "vikunja-mcp",
      "env": {
        "VIKUNJA_URL": "https://vikunja.example.com",
        "VIKUNJA_TOKEN": "tk_your_token_here"
      }
    }
  }
}
```

### Stdio (default)

```bash
VIKUNJA_URL=https://vikunja.example.com VIKUNJA_TOKEN=tk_... vikunja-mcp
```

### HTTP transport

```bash
VIKUNJA_URL=https://vikunja.example.com VIKUNJA_TOKEN=tk_... vikunja-mcp -http :8080
```

The MCP endpoint will be available at `http://localhost:8080/mcp`.

## Tools

### User
- `get_current_user` — get the authenticated user's profile (includes `bot_owner_id` for bot detection)

### Projects
- `list_projects` — list all projects (search, pagination)
- `get_project` — get project by ID
- `create_project` — create a new project
- `update_project` — update project fields (partial update)
- `delete_project` — delete a project

### Tasks
- `list_tasks` — list tasks across all projects or within a specific project (filter, sort, search, pagination)
- `get_task` — get task by ID (includes relations and reminders)
- `create_task` — create a task in a project (supports reminders)
- `update_task` — update task fields (partial update, supports reminders)
- `delete_task` — delete a task
- `complete_task` — mark a task as done
- `search_tasks` — search tasks by query string with optional filters

### Labels
- `list_labels` — list all labels
- `create_label` — create a label
- `delete_label` — delete a label
- `add_label_to_task` — add label to task
- `remove_label_from_task` — remove label from task

### Comments
- `list_comments` — list comments on a task
- `add_comment` — add comment to a task

### Assignees
- `add_assignee` — assign user to task
- `remove_assignee` — unassign user from task

### Relations
- `create_task_relation` — create a relation between tasks (subtask, blocking, related, etc.)
- `delete_task_relation` — delete a task relation

### Views & Kanban
- `list_views` — list all views for a project (list, kanban, gantt, table)
- `create_view` — create a new view for a project
- `update_view` — update a view's fields
- `delete_view` — delete a view
- `list_buckets` — list buckets and their tasks for a kanban view
- `move_task_to_bucket` — move a task to a different bucket

### Power Queries
- `overdue_tasks` — list tasks past their due date
- `due_today` — list tasks due today
- `due_this_week` — list tasks due within 7 days
- `high_priority_tasks` — list open tasks with priority 3+ (high)
- `urgent_tasks` — list open tasks with priority 4+ (urgent)
- `focus_now` — list tasks needing immediate attention (urgent or overdue)
- `unscheduled_tasks` — list open tasks with no due date
- `upcoming_deadlines` — list tasks due within N days (default 7)
- `task_summary` — get counts of overdue, due today, high priority, and total open tasks

### Batch Operations
- `batch_create_tasks` — create multiple tasks in a single project
- `batch_update_tasks` — update multiple tasks at once

### Time Entries

Time tracking requires a [Vikunja license](https://vikunja.io/pricing). Time entry tools are not included in this MCP server.

## Resources

The server also exposes read-only MCP resources for browsing:

- `vikunja://projects` — all projects
- `vikunja://projects/{id}` — a specific project
- `vikunja://projects/{project_id}/tasks` — tasks in a project
- `vikunja://tasks/{id}` — a specific task

## License

Apache
