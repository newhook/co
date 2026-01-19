package github

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestPostPRComment(t *testing.T) {
	// This test requires a real PR URL and GitHub authentication
	// It's meant to be run manually for verification

	// Skip if not in manual test mode
	if os.Getenv("MANUAL_GITHUB_TEST") != "1" {
		t.Skip("Skipping manual GitHub test. Set MANUAL_GITHUB_TEST=1 to run")
	}

	// You need to set a real PR URL here when testing manually
	prURL := os.Getenv("TEST_PR_URL")
	if prURL == "" {
		t.Fatal("TEST_PR_URL environment variable must be set for manual testing")
	}

	client := NewClient()
	ctx := context.Background()

	// Test posting a simple comment
	err := client.PostPRComment(ctx, prURL, "Test comment from co integration test")
	if err != nil {
		t.Fatalf("Failed to post PR comment: %v", err)
	}

	t.Log("Successfully posted PR comment")
}

func TestPostReplyToComment(t *testing.T) {
	// This test requires a real PR URL and comment ID
	// It's meant to be run manually for verification

	// Skip if not in manual test mode
	if os.Getenv("MANUAL_GITHUB_TEST") != "1" {
		t.Skip("Skipping manual GitHub test. Set MANUAL_GITHUB_TEST=1 to run")
	}

	prURL := os.Getenv("TEST_PR_URL")
	if prURL == "" {
		t.Fatal("TEST_PR_URL environment variable must be set for manual testing")
	}

	// You need to set a real comment ID here when testing manually
	commentID := 123456789 // Replace with actual comment ID

	client := NewClient()
	ctx := context.Background()

	// Test posting a reply to a comment
	err := client.PostReplyToComment(ctx, prURL, commentID, "Test reply from co integration test")
	if err != nil {
		t.Fatalf("Failed to post reply to comment: %v", err)
	}

	t.Log("Successfully posted reply to comment")
}

func TestCommentIntegration(t *testing.T) {
	// This test demonstrates the full flow of creating a bead from a comment
	// and posting back an acknowledgment

	// Skip if not in manual test mode
	if os.Getenv("MANUAL_GITHUB_TEST") != "1" {
		t.Skip("Skipping manual GitHub test. Set MANUAL_GITHUB_TEST=1 to run")
	}

	prURL := os.Getenv("TEST_PR_URL")
	if prURL == "" {
		t.Fatal("TEST_PR_URL environment variable must be set for manual testing")
	}

	client := NewClient()
	ctx := context.Background()

	// Simulate creating a bead from feedback
	beadID := "beads-test-123"
	feedbackTitle := "Fix test failure in authentication module"
	priority := 2

	// Post acknowledgment message
	ackMessage := fmt.Sprintf("✅ Created tracking issue **%s** for this feedback.\n\nTitle: %s\nPriority: P%d",
		beadID, feedbackTitle, priority)

	err := client.PostPRComment(ctx, prURL, ackMessage)
	if err != nil {
		t.Fatalf("Failed to post acknowledgment: %v", err)
	}

	t.Log("Successfully posted bead acknowledgment to PR")
}