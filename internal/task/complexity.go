package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
)

// LLMEstimator uses Claude Haiku to estimate bead complexity.
type LLMEstimator struct {
	client   anthropic.Client
	database *db.DB
}

// NewLLMEstimator creates a new LLM-based complexity estimator.
// The API key is read from the ANTHROPIC_API_KEY environment variable.
func NewLLMEstimator(database *db.DB) *LLMEstimator {
	client := anthropic.NewClient()
	return &LLMEstimator{
		client:   client,
		database: database,
	}
}

// Estimate returns a complexity score (1-10) and estimated context tokens for a bead.
// Results are cached based on the description hash.
func (e *LLMEstimator) Estimate(bead beads.Bead) (score int, tokens int, err error) {
	// Calculate description hash for caching
	descHash := hashDescription(bead.Title, bead.Description)

	// Check cache first
	if e.database != nil {
		cached, err := e.getCachedComplexity(bead.ID, descHash)
		if err == nil && cached != nil {
			return cached.ComplexityScore, cached.EstimatedTokens, nil
		}
	}

	// Call LLM to estimate complexity
	score, tokens, err = e.callLLM(bead)
	if err != nil {
		return 0, 0, err
	}

	// Cache the result
	if e.database != nil {
		if err := e.cacheComplexity(bead.ID, descHash, score, tokens); err != nil {
			// Log but don't fail on cache error
			fmt.Printf("Warning: failed to cache complexity: %v\n", err)
		}
	}

	return score, tokens, nil
}

// hashDescription creates a SHA256 hash of the title and description.
func hashDescription(title, description string) string {
	h := sha256.New()
	h.Write([]byte(title))
	h.Write([]byte(description))
	return hex.EncodeToString(h.Sum(nil))
}

// getCachedComplexity retrieves cached complexity if the hash matches.
func (e *LLMEstimator) getCachedComplexity(beadID, descHash string) (*BeadComplexity, error) {
	row := e.database.QueryRow(`
		SELECT complexity_score, estimated_tokens FROM complexity_cache
		WHERE bead_id = ? AND description_hash = ?
	`, beadID, descHash)

	var score, tokens int
	if err := row.Scan(&score, &tokens); err != nil {
		return nil, err
	}

	return &BeadComplexity{
		BeadID:          beadID,
		ComplexityScore: score,
		EstimatedTokens: tokens,
	}, nil
}

// cacheComplexity stores complexity estimate in the cache.
func (e *LLMEstimator) cacheComplexity(beadID, descHash string, score, tokens int) error {
	_, err := e.database.Exec(`
		INSERT INTO complexity_cache (bead_id, description_hash, complexity_score, estimated_tokens)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(bead_id) DO UPDATE SET
			description_hash = ?,
			complexity_score = ?,
			estimated_tokens = ?
	`, beadID, descHash, score, tokens, descHash, score, tokens)
	return err
}

// complexityResponse is the expected JSON response from the LLM.
type complexityResponse struct {
	Score  int `json:"score"`
	Tokens int `json:"tokens"`
}

// callLLM makes an API call to Claude Haiku to estimate complexity.
func (e *LLMEstimator) callLLM(bead beads.Bead) (int, int, error) {
	prompt := fmt.Sprintf(`Analyze this software development task and estimate its complexity.

Title: %s

Description:
%s

Respond with a JSON object containing:
- "score": complexity score from 1-10 (1=trivial fix, 5=medium feature, 10=major refactor)
- "tokens": estimated context tokens needed to complete this task (typically 5000-50000)

Consider:
- Code changes required
- Number of files likely affected
- Testing requirements
- Potential for edge cases

Respond ONLY with the JSON object, no other text.`, bead.Title, bead.Description)

	ctx := context.Background()
	response, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude3_5Haiku20241022,
		MaxTokens: 100,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to call Anthropic API: %w", err)
	}

	// Extract text from response
	if len(response.Content) == 0 {
		return 0, 0, fmt.Errorf("empty response from Anthropic API")
	}

	var text string
	for _, block := range response.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}

	if text == "" {
		return 0, 0, fmt.Errorf("no text in Anthropic API response")
	}

	// Parse JSON response
	var result complexityResponse
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return 0, 0, fmt.Errorf("failed to parse complexity response: %w (response: %s)", err, text)
	}

	// Validate score range
	if result.Score < 1 {
		result.Score = 1
	} else if result.Score > 10 {
		result.Score = 10
	}

	// Ensure reasonable token estimate
	if result.Tokens < 1000 {
		result.Tokens = 1000
	} else if result.Tokens > 100000 {
		result.Tokens = 100000
	}

	return result.Score, result.Tokens, nil
}
