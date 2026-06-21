# vikunja-mcp

An [MCP](https://modelcontextprotocol.io/) server that wraps the [Vikunja](https://vikunja.io/) v2 REST API, enabling AI agents to manage tasks, projects, labels, time entries, and more.

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
| `VIKUNJA_TOKEN` | API token (starts with `tk_`). Create one in Vikunja under Settings > API Tokens. |

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

### Projects
- `list_projects` — list all projects (search, pagination)
- `get_project` — get project by ID
- `create_project` — create a new project

### Tasks
- `list_tasks` — list tasks across all projects or within a specific project (filter, sort, search, pagination)
- `get_task` — get task by ID
- `create_task` — create a task in a project
- `update_task` — update task fields (partial update)
- `delete_task` — delete a task
- `complete_task` — mark a task as done
- `search_tasks` — search tasks by query string with optional filters

### Labels
- `list_labels` — list all labels
- `create_label` — create a label
- `add_label_to_task` — add label to task
- `remove_label_from_task` — remove label from task

### Comments
- `list_comments` — list comments on a task
- `add_comment` — add comment to a task

### Assignees
- `add_assignee` — assign user to task
- `remove_assignee` — unassign user from task

### Time Entries
- `list_time_entries` — list time entries (filterable by date, project, task)
- `create_time_entry` — log time or start a timer
- `update_time_entry` — update a time entry
- `delete_time_entry` — delete a time entry
- `stop_timer` — stop the running timer

## Resources

The server also exposes read-only MCP resources for browsing:

- `vikunja://projects` — all projects
- `vikunja://projects/{id}` — a specific project
- `vikunja://projects/{project_id}/tasks` — tasks in a project
- `vikunja://tasks/{id}` — a specific task

## License

Apache
