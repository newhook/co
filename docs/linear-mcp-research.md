# Linear MCP Integration Research

## Overview

This document provides comprehensive research on integrating with Linear's Model Context Protocol (MCP) server to enable querying and managing Linear issues.

## MCP Server Endpoints

Linear provides an official, centrally-hosted MCP server with two transport options:

- **HTTP (Streamable)**: `https://mcp.linear.app/mcp` (recommended for reliability)
- **SSE**: `https://mcp.linear.app/sse`

## Available Tools/Methods

The Linear MCP server provides 24 specialized tools covering comprehensive Linear project management functionality.

**Note**: Tool names separated by `/` indicate aliases (the same tool accessible by multiple names).

### Issue Management Tools

1. **list_issues** - Browse and filter Linear issues
   - Parameters:
     - `assignedToMe` (boolean, default: false) - Filter issues assigned to current user
     - `assignee` (string) - Filter by specific assignee name
     - `status` (string) - Filter by issue status/state name
     - `project` (string) - Filter by project name
     - `sortBy` (enum: "createdAt" | "updatedAt", default: "createdAt")
     - `sortDirection` (enum: "ASC" | "DESC", default: "DESC")
     - `limit` (number, 1-100, default: 25) - Results limit
     - `debug` (boolean, default: false) - Enable diagnostic output
   - Returns: Formatted list of issues with all fields

2. **list_my_issues** - Personal task management with priority tracking

3. **get_issue** - Retrieve detailed issue data
   - Accepts: Issue identifier (e.g., "ENG-123") or full Linear URL
   - Returns: Comprehensive issue details including:
     - Title, description, status, priority, assignee
     - Attachments and git branches
     - Comments and metadata

4. **create_issue** - Create a new Linear issue
   - Required parameters:
     - `title` (string) - Issue title
     - `teamId` (string) - Team identifier
   - Optional parameters:
     - `description` (string) - Issue description (supports markdown)
     - `assigneeId` (string) - Assignee identifier
     - `priority` (number, 0-4) - Priority level:
       - 0: No priority
       - 1: Urgent
       - 2: High
       - 3: Medium
       - 4: Low
     - `stateId` (string) - Workflow state identifier
     - `estimate` (number) - Issue estimate
     - `labelIds` (array of strings) - Label identifiers

5. **update_issue** - Update an existing issue
   - Required: `issueId`
   - Optional fields: `title`, `description`, `status`, `assigneeId`, `priority`
   - Note: Status expects status NAME (e.g., "In Progress"), not ID
   - Assignee can use 'me' for self-assignment

6. **delete_issue** - Delete an existing issue

7. **search_issues** - Advanced issue search
   - Supports text queries and filters
   - Filter options: assignee, creator, project, status, priority, dates, labels
   - Logical operators and relative date filtering supported

### Comment Tools

8. **list_comments** - List comments on an issue
9. **create_comment** / **add_comment** - Create markdown-formatted comments on issues

### Project & Team Tools

10. **list_projects** - Get list of projects with name filtering and pagination
11. **get_project** - Get specific project details
12. **create_project** - Create new project
13. **update_project** - Update project details
14. **get_project_updates** - Get project updates with filtering
15. **create_project_update** - Create project update

16. **list_teams** / **get_teams** - List teams with name/key filtering
17. **get_team** - Get specific team details

18. **list_users** - List team members
19. **get_user** - Get specific user details

### Workflow & Documentation

20. **get_workflow_states** / **list_issue_statuses** - List workflow states/statuses for a team
21. **get_issue_status** - Get specific status details
22. **list_issue_labels** - Advanced categorization and filtering

23. **get_document** / **list_documents** - Documentation integration
24. **search_documentation** - AI-powered Linear feature discovery

## Issue Fields Returned

When querying issues (e.g., via `get_issue` or `list_issues`), the following fields are returned:

- **title** - Issue name
- **id** - Unique UUID identifier
- **identifier** - Human-readable issue key (e.g., "TEAM-123")
- **url** - Direct link to issue in Linear
- **description** - Issue description (markdown)
- **status** / **state** - Current workflow state
- **priority** - Priority level (0-4, mapped to: No priority, Urgent, High, Medium, Low)
- **project** - Associated project name (if any)
- **assignee** - Person assigned to the issue
- **createdAt** - Creation timestamp
- **updatedAt** - Last update timestamp
- **labels** - Array of labels
- **estimate** - Estimated effort
- **attachments** - Attached files/images
- **gitBranches** - Associated git branches (if any)
- **comments** - Issue comments (when using `get_issue_with_comments`)

## Querying Issues by ID or URL

The `get_issue` tool supports two input formats:

1. **Issue Identifier**: `"ENG-123"` (team prefix + number)
2. **Full URL**: `"https://linear.app/company/issue/ENG-123/issue-title"`

Both formats retrieve the same comprehensive issue data.

## Authentication

Linear MCP uses **OAuth 2.1 with dynamic client registration**.

### Authentication Methods

1. **Interactive OAuth Flow**: Standard OAuth when connecting your AI client
2. **Direct Token Method**: Pass OAuth token or API key directly in header:
   ```
   Authorization: Bearer <yourtoken>
   ```

### API Key Setup

For custom implementations or direct API access:
- Create API key from team settings page
- Configure via `LINEAR_API_KEY` environment variable
- Supports both personal API keys and OAuth app tokens

### Troubleshooting

Clear cached credentials if encountering authentication errors:
```bash
rm -rf ~/.mcp-auth
```

## Rate Limiting

Linear enforces both request-based and complexity-based rate limits:

### Request Limits

- **API Key Authentication**: 1,500 requests per hour
- **OAuth App Authentication**: 500 requests per hour per user/app
- **Unauthenticated**: 60 requests per hour

### Complexity Limits

- **API Key**: 250,000 complexity points per hour
- **OAuth App**: 200,000 complexity points per hour per user/app
- **Unauthenticated**: 10,000 complexity points per hour per IP
- **Maximum Single Query**: 10,000 points (hard limit)

### Complexity Calculation

- Each property: 0.1 point
- Each object: 1 point
- Connections multiply children's points by pagination argument (default: 50)
- Score rounded up to nearest integer

### Rate Limiter Algorithm

Uses **leaky bucket algorithm** - tokens refill at a constant rate.

### Important Notes

- Requests are associated with the authenticated user (shared quota across API keys)
- Some endpoints have individual rate limits lower than global limits
- Polling discouraged - use webhooks for data updates
- Higher limits available on request (contact Linear support)

## Integration Setup

### Configuration Example

For clients like Claude Desktop, Cursor, or Windsurf:

```json
{
  "mcpServers": {
    "linear": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://mcp.linear.app/mcp"]
    }
  }
}
```

### Supported Platforms

- Claude Desktop (free/pro and enterprise)
- Cursor
- Windsurf
- VS Code
- Zed

### Requirements

- Node.js installation (for some implementations)
- Linear API key or OAuth authentication

## Technical Details

- **Protocol**: Follows authenticated remote MCP specification
- **Transport**: Supports both SSE and Streamable HTTP
- **Security**: Enterprise-grade via OAuth 2.1
- **Scalability**: Handles large teams and complex hierarchies
- **Real-time**: Synchronization between Linear and AI assistants
- **Compatibility**: Works with Claude, ChatGPT, Cursor, and other MCP-compatible clients

## Community Implementations

While Linear provides an official MCP server, several community implementations exist:

1. **jerhadf/linear-mcp-server** - DEPRECATED (migrate to official server)
2. **keegancsmith/linear-issues-mcp-server** - Read-only access implementation
3. **tacticlaunch/mcp-linear** - Full-featured community implementation
4. **Other implementations**: @cosmix/linear-mcp, geropl/linear-mcp-go

**Recommendation**: Use the official Linear MCP server at `https://mcp.linear.app/mcp` for best compatibility and ongoing support.

## Use Cases for Integration

Based on the available tools, the Linear MCP integration enables:

1. **Issue Querying**: Fetch issues by ID, URL, or advanced search criteria
2. **Issue Creation**: Create new issues with full metadata
3. **Issue Updates**: Modify existing issues programmatically
4. **Comment Management**: Add and retrieve issue comments
5. **Project Tracking**: Access project information and updates
6. **Team Coordination**: List users, teams, and assignments
7. **Workflow Management**: Query and update workflow states
8. **Documentation Access**: Search and retrieve Linear documentation

## Limitations & Considerations

1. **Rate Limits**: Must respect hourly request and complexity limits
2. **Authentication**: Requires OAuth or API key setup
3. **Complexity**: Complex queries count heavily against limits
4. **Polling**: Discouraged in favor of webhooks for real-time updates
5. **Schema Access**: Full GraphQL schema not exposed via MCP (abstracted through tools)

## References

- [Linear MCP Documentation](https://linear.app/docs/mcp)
- [Linear API Rate Limiting](https://linear.app/developers/rate-limiting)
- [Linear Developers](https://linear.app/developers)
- [Linear MCP Changelog](https://linear.app/changelog/2025-05-01-mcp) (official announcement: May 1, 2025)
- [Remote MCP - Linear](https://www.remote-mcp.com/servers/linear)
- [Glama MCP Directory](https://glama.ai/mcp/servers/@cosmix/linear-mcp)
- [Apidog Linear MCP Guide](https://apidog.com/blog/linear-mcp-server/)

---

**Last Updated**: 2026-01-15
**Research Completed For**: Bead ac-kqy9.1 - Research Linear MCP integration capabilities
