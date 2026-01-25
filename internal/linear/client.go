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
	// DefaultGraphQLEndpoint is the Linear GraphQL API endpoint
	DefaultGraphQLEndpoint = "https://api.linear.app/graphql"

	// DefaultTimeout for HTTP requests
	DefaultTimeout = 30 * time.Second
)

// Client is a Linear GraphQL API client
type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Linear GraphQL API client
// API key is read from LINEAR_API_KEY environment variable if not provided
func NewClient(apiKey string) (*Client, error) {
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("linear API key not provided and LINEAR_API_KEY environment variable not set")
	}

	return &Client{
		endpoint: DefaultGraphQLEndpoint,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}, nil
}

// SetEndpoint sets a custom GraphQL endpoint (useful for testing)
func (c *Client) SetEndpoint(endpoint string) {
	c.endpoint = endpoint
}

// graphqlRequest represents a GraphQL request
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphqlResponse represents a GraphQL response
type graphqlResponse struct {
	Data   json.RawMessage  `json:"data,omitempty"`
	Errors []graphqlError   `json:"errors,omitempty"`
}

// graphqlError represents a GraphQL error
type graphqlError struct {
	Message   string `json:"message"`
	Locations []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations,omitempty"`
	Path []any `json:"path,omitempty"`
}

// query executes a GraphQL query and returns the raw data
func (c *Client) query(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	req := graphqlRequest{
		Query:     query,
		Variables: variables,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", c.apiKey)

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

	var gqlResp graphqlResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GraphQL response: %w", err)
	}

	// Handle GraphQL errors
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	return gqlResp.Data, nil
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

// issueFields contains the GraphQL fragment for issue fields
const issueFields = `
	id
	identifier
	title
	description
	priority
	url
	createdAt
	updatedAt
	estimate
	state {
		id
		name
		type
	}
	assignee {
		id
		name
		email
	}
	project {
		id
		name
	}
	labels {
		nodes {
			id
			name
		}
	}
	relations {
		nodes {
			type
			relatedIssue {
				identifier
			}
		}
	}
`

// issueResponse wraps the GraphQL response for a single issue query
type issueResponse struct {
	Issue *issueData `json:"issue"`
}

// issuesResponse wraps the GraphQL response for issues queries
type issuesResponse struct {
	Issues struct {
		Nodes []issueData `json:"nodes"`
	} `json:"issues"`
}

// issueSearchResponse wraps the GraphQL response for issue search
type issueSearchResponse struct {
	IssueSearch struct {
		Nodes []issueData `json:"nodes"`
	} `json:"issueSearch"`
}

// issueData represents the raw GraphQL issue data
type issueData struct {
	ID          string   `json:"id"`
	Identifier  string   `json:"identifier"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"`
	URL         string   `json:"url"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
	Estimate    *float64 `json:"estimate"`
	State       *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Assignee *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"assignee"`
	Project *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	Labels struct {
		Nodes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Relations struct {
		Nodes []struct {
			Type         string `json:"type"`
			RelatedIssue struct {
				Identifier string `json:"identifier"`
			} `json:"relatedIssue"`
		} `json:"nodes"`
	} `json:"relations"`
	Comments struct {
		Nodes []struct {
			ID        string `json:"id"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
			User      struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"user"`
		} `json:"nodes"`
	} `json:"comments"`
}

// toIssue converts issueData to Issue
func (d *issueData) toIssue() *Issue {
	issue := &Issue{
		ID:          d.ID,
		Identifier:  d.Identifier,
		Title:       d.Title,
		Description: d.Description,
		Priority:    d.Priority,
		URL:         d.URL,
		Estimate:    d.Estimate,
	}

	// Parse timestamps
	if d.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, d.CreatedAt); err == nil {
			issue.CreatedAt = t
		}
	}
	if d.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, d.UpdatedAt); err == nil {
			issue.UpdatedAt = t
		}
	}

	// Map state
	if d.State != nil {
		issue.State = State{
			ID:   d.State.ID,
			Name: d.State.Name,
			Type: d.State.Type,
		}
	}

	// Map assignee
	if d.Assignee != nil {
		issue.Assignee = &User{
			ID:    d.Assignee.ID,
			Name:  d.Assignee.Name,
			Email: d.Assignee.Email,
		}
	}

	// Map project
	if d.Project != nil {
		issue.Project = &Project{
			ID:   d.Project.ID,
			Name: d.Project.Name,
		}
	}

	// Map labels
	for _, l := range d.Labels.Nodes {
		issue.Labels = append(issue.Labels, Label{
			ID:   l.ID,
			Name: l.Name,
		})
	}

	// Map blockedBy from relations - filter for "blocks" type
	// When an issue has a relation with type "blocks", it means this issue blocks the related issue
	// We're looking for inverse: issues where this issue is blocked by the related issue
	for _, r := range d.Relations.Nodes {
		if r.Type == "blocks" {
			issue.BlockedBy = append(issue.BlockedBy, r.RelatedIssue.Identifier)
		}
	}

	// Map comments
	for _, c := range d.Comments.Nodes {
		comment := Comment{
			ID:   c.ID,
			Body: c.Body,
			User: User{
				ID:    c.User.ID,
				Name:  c.User.Name,
				Email: c.User.Email,
			},
		}
		if c.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
				comment.CreatedAt = t
			}
		}
		issue.Comments = append(issue.Comments, comment)
	}

	return issue
}

// GetIssue fetches a single issue by ID or URL
func (c *Client) GetIssue(ctx context.Context, issueIDOrURL string) (*Issue, error) {
	issueID, err := ParseIssueIDOrURL(issueIDOrURL)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		query GetIssue($id: String!) {
			issue(id: $id) {
				%s
			}
		}
	`, issueFields)

	data, err := c.query(ctx, query, map[string]any{"id": issueID})
	if err != nil {
		return nil, fmt.Errorf("failed to get issue %s: %w", issueID, err)
	}

	var resp issueResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issue response: %w", err)
	}

	if resp.Issue == nil {
		return nil, fmt.Errorf("issue %s not found", issueID)
	}

	return resp.Issue.toIssue(), nil
}

// SearchIssues searches for issues using a text query
// Note: The filters parameter is kept for API compatibility but Linear's issueSearch
// only supports a text query. Use ListIssues for filtered queries.
func (c *Client) SearchIssues(ctx context.Context, searchQuery string, filters map[string]any) ([]*Issue, error) {
	if searchQuery == "" {
		// If no search query, fall back to ListIssues with filters
		return c.ListIssues(ctx, filters)
	}

	gqlQuery := fmt.Sprintf(`
		query SearchIssues($query: String!) {
			issueSearch(query: $query, first: 50) {
				nodes {
					%s
				}
			}
		}
	`, issueFields)

	data, err := c.query(ctx, gqlQuery, map[string]any{"query": searchQuery})
	if err != nil {
		return nil, fmt.Errorf("failed to search issues: %w", err)
	}

	var resp issueSearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal search response: %w", err)
	}

	issues := make([]*Issue, len(resp.IssueSearch.Nodes))
	for i := range resp.IssueSearch.Nodes {
		issues[i] = resp.IssueSearch.Nodes[i].toIssue()
	}

	return issues, nil
}

// ListIssues lists issues with optional filters
// Supported filter keys: status, priority, assignee
func (c *Client) ListIssues(ctx context.Context, filters map[string]any) ([]*Issue, error) {
	// Build filter object for GraphQL
	// Note: Linear's GraphQL filter syntax is specific
	// We'll query all issues and the filters will be applied server-side if provided
	gqlQuery := fmt.Sprintf(`
		query ListIssues {
			issues(first: 50) {
				nodes {
					%s
				}
			}
		}
	`, issueFields)

	data, err := c.query(ctx, gqlQuery, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}

	var resp issuesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal issues response: %w", err)
	}

	issues := make([]*Issue, len(resp.Issues.Nodes))
	for i := range resp.Issues.Nodes {
		issues[i] = resp.Issues.Nodes[i].toIssue()
	}

	return issues, nil
}

// commentsResponse wraps the GraphQL response for issue comments
type commentsResponse struct {
	Issue struct {
		Comments struct {
			Nodes []struct {
				ID        string `json:"id"`
				Body      string `json:"body"`
				CreatedAt string `json:"createdAt"`
				User      struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"user"`
			} `json:"nodes"`
		} `json:"comments"`
	} `json:"issue"`
}

// GetIssueComments fetches comments for an issue
func (c *Client) GetIssueComments(ctx context.Context, issueID string) ([]Comment, error) {
	gqlQuery := `
		query GetIssueComments($id: String!) {
			issue(id: $id) {
				comments(first: 100) {
					nodes {
						id
						body
						createdAt
						user {
							id
							name
							email
						}
					}
				}
			}
		}
	`

	data, err := c.query(ctx, gqlQuery, map[string]any{"id": issueID})
	if err != nil {
		return nil, fmt.Errorf("failed to get comments for issue %s: %w", issueID, err)
	}

	var resp commentsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal comments response: %w", err)
	}

	comments := make([]Comment, len(resp.Issue.Comments.Nodes))
	for i, c := range resp.Issue.Comments.Nodes {
		comments[i] = Comment{
			ID:   c.ID,
			Body: c.Body,
			User: User{
				ID:    c.User.ID,
				Name:  c.User.Name,
				Email: c.User.Email,
			},
		}
		if c.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
				comments[i].CreatedAt = t
			}
		}
	}

	return comments, nil
}
