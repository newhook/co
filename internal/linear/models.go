package linear

import "time"

// Issue represents a Linear issue with all relevant fields for import
type Issue struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"` // e.g., "ENG-123"
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    int       `json:"priority"` // 0-4
	State       State     `json:"state"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// Optional fields
	Assignee *User     `json:"assignee,omitempty"`
	Project  *Project  `json:"project,omitempty"`
	Labels   []Label   `json:"labels,omitempty"`
	Estimate *float64  `json:"estimate,omitempty"`
	Comments []Comment `json:"comments,omitempty"`

	// Dependencies
	BlockedBy []string `json:"blockedBy,omitempty"` // Issue IDs
}

// State represents the workflow state of an issue
type State struct {
	ID   string `json:"id"`
	Name string `json:"name"` // e.g., "In Progress", "Done", "Todo"
	Type string `json:"type"` // e.g., "unstarted", "started", "completed", "canceled"
}

// User represents a Linear user
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Project represents a Linear project
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Label represents a Linear label
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Comment represents a Linear comment
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	User      User      `json:"user"`
}

// MCPRequest represents a request to the Linear MCP server
type MCPRequest struct {
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
}

// MCPResponse represents a response from the Linear MCP server
type MCPResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      int       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *MCPError `json:"error,omitempty"`
}

// MCPError represents an error from the Linear MCP server
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// ImportResult represents the result of importing a Linear issue
type ImportResult struct {
	LinearID   string
	LinearURL  string
	BeadID     string
	Success    bool
	Error      error
	SkipReason string // e.g., "already imported", "filtered out"
}

// ImportOptions represents options for importing Linear issues
type ImportOptions struct {
	DryRun         bool
	UpdateExisting bool   // Update existing beads if already imported
	CreateDeps     bool
	MaxDepDepth    int
	AssigneeFilter string
	StatusFilter   string
	PriorityFilter string
	TypeFilter     string
}
