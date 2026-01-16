package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultMCPEndpoint is the Linear MCP HTTP endpoint
	DefaultMCPEndpoint = "https://mcp.linear.app/mcp"

	// DefaultTimeout for HTTP requests
	DefaultTimeout = 30 * time.Second
)

// Client is a Linear MCP client
type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
	nextID     int
}

// NewClient creates a new Linear MCP client
// API key is read from LINEAR_API_KEY environment variable if not provided
func NewClient(apiKey string) (*Client, error) {
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Linear API key not provided and LINEAR_API_KEY environment variable not set")
	}

	return &Client{
		endpoint: DefaultMCPEndpoint,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		nextID: 1,
	}, nil
}

// SetEndpoint sets a custom MCP endpoint (useful for testing)
func (c *Client) SetEndpoint(endpoint string) {
	c.endpoint = endpoint
}

// callTool invokes an MCP tool and returns the raw result
func (c *Client) callTool(ctx context.Context, toolName string, params map[string]any) (any, error) {
	req := MCPRequest{
		Method:  "tools/call",
		JSONRPC: "2.0",
		ID:      c.nextID,
		Params: map[string]any{
			"name":      toolName,
			"arguments": params,
		},
	}
	c.nextID++

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	var mcpResp MCPResponse
	if err := json.Unmarshal(body, &mcpResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MCP response: %w", err)
	}

	// Handle MCP errors
	if mcpResp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s (data: %s)", mcpResp.Error.Code, mcpResp.Error.Message, mcpResp.Error.Data)
	}

	return mcpResp.Result, nil
}

// ParseIssueIDOrURL extracts the Linear issue identifier from a URL or ID string
// Accepts formats:
//   - Issue ID: "ENG-123", "eng-123"
//   - Issue URL: "https://linear.app/myteam/issue/ENG-123/..."
func ParseIssueIDOrURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty input")
	}

	// If it looks like a URL, extract the identifier
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		// Pattern: https://linear.app/<team>/issue/<IDENTIFIER>/...
		re := regexp.MustCompile(`linear\.app/[^/]+/issue/([A-Za-z]+-\d+)`)
		matches := re.FindStringSubmatch(input)
		if len(matches) < 2 {
			return "", fmt.Errorf("could not extract issue identifier from URL: %s", input)
		}
		return strings.ToUpper(matches[1]), nil
	}

	// Otherwise, validate it as an issue identifier (e.g., ENG-123)
	re := regexp.MustCompile(`^[A-Za-z]+-\d+$`)
	if !re.MatchString(input) {
		return "", fmt.Errorf("invalid Linear issue identifier format: %s (expected format: TEAM-123)", input)
	}

	return strings.ToUpper(input), nil
}

// GetIssue fetches a single issue by ID or URL
func (c *Client) GetIssue(ctx context.Context, issueIDOrURL string) (*Issue, error) {
	issueID, err := ParseIssueIDOrURL(issueIDOrURL)
	if err != nil {
		return nil, err
	}

	result, err := c.callTool(ctx, "get_issue", map[string]any{
		"id": issueID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get issue %s: %w", issueID, err)
	}

	// Parse the result into an Issue struct
	// The MCP response structure varies, so we need to handle different formats
	var issue Issue
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &issue); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issue: %w", err)
	}

	// Ensure identifier and URL are set
	if issue.Identifier == "" {
		issue.Identifier = issueID
	}
	if issue.URL == "" {
		// Construct URL from identifier if not provided
		// Note: We don't know the team slug, so this is a best-effort guess
		issue.URL = fmt.Sprintf("https://linear.app/issue/%s", issueID)
	}

	return &issue, nil
}

// SearchIssues searches for issues using a query and filters
func (c *Client) SearchIssues(ctx context.Context, query string, filters map[string]any) ([]*Issue, error) {
	params := map[string]any{}
	if query != "" {
		params["query"] = query
	}
	for k, v := range filters {
		params[k] = v
	}

	result, err := c.callTool(ctx, "search_issues", params)
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}

	// Parse the result into a slice of Issues
	var issues []*Issue
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &issues); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issues: %w", err)
	}

	return issues, nil
}

// ListIssues lists issues with optional filters
func (c *Client) ListIssues(ctx context.Context, filters map[string]any) ([]*Issue, error) {
	result, err := c.callTool(ctx, "list_issues", filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}

	// Parse the result into a slice of Issues
	var issues []*Issue
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &issues); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issues: %w", err)
	}

	return issues, nil
}

// GetIssueComments fetches comments for an issue
func (c *Client) GetIssueComments(ctx context.Context, issueID string) ([]Comment, error) {
	result, err := c.callTool(ctx, "list_comments", map[string]any{
		"issueId": issueID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get comments for issue %s: %w", issueID, err)
	}

	// Parse the result into a slice of Comments
	var comments []Comment
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := json.Unmarshal(resultBytes, &comments); err != nil {
		return nil, fmt.Errorf("failed to unmarshal comments: %w", err)
	}

	return comments, nil
}
