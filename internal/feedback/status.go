package feedback

import (
	"context"
	"fmt"

	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/github"
)

// UpdatePRStatusIfChanged compares the new PR status with the stored status
// and updates the database if anything changed. Returns true if status changed.
func UpdatePRStatusIfChanged(ctx context.Context, database *db.DB, work *db.Work, newStatus *github.PRStatusInfo, quiet bool) bool {
	// Get current approvers from work (stored as JSON)
	currentApprovers := github.ApproversFromJSON(work.Approvers)

	// Check if anything changed
	ciChanged := work.CIStatus != newStatus.CIStatus
	approvalChanged := work.ApprovalStatus != newStatus.ApprovalStatus
	approversChanged := !stringSlicesEqual(currentApprovers, newStatus.Approvers)
	prStateChanged := work.PRState != newStatus.PRState

	if !ciChanged && !approvalChanged && !approversChanged && !prStateChanged {
		// No changes
		if !quiet {
			fmt.Printf("PR status unchanged: CI=%s, Approval=%s, State=%s\n", work.CIStatus, work.ApprovalStatus, work.PRState)
		}
		return false
	}

	// Log what changed
	if !quiet {
		if ciChanged {
			fmt.Printf("CI status changed: %s -> %s\n", work.CIStatus, newStatus.CIStatus)
		}
		if approvalChanged {
			fmt.Printf("Approval status changed: %s -> %s\n", work.ApprovalStatus, newStatus.ApprovalStatus)
		}
		if approversChanged {
			fmt.Printf("Approvers changed: %v -> %v\n", currentApprovers, newStatus.Approvers)
		}
		if prStateChanged {
			fmt.Printf("PR state changed: %s -> %s\n", work.PRState, newStatus.PRState)
		}
	}

	// Convert approvers to JSON
	approversJSON := github.ApproversToJSON(newStatus.Approvers)

	// Update the database
	if err := database.UpdateWorkPRStatus(ctx, work.ID, newStatus.CIStatus, newStatus.ApprovalStatus, approversJSON, newStatus.PRState); err != nil {
		if !quiet {
			fmt.Printf("Warning: failed to update PR status: %v\n", err)
		}
		return false
	}

	// If PR was merged, transition work to merged status
	if newStatus.PRState == db.PRStateMerged && work.Status != db.StatusMerged {
		if !quiet {
			fmt.Printf("PR merged! Transitioning work %s to merged status\n", work.ID)
		}
		if err := database.MergeWork(ctx, work.ID); err != nil {
			if !quiet {
				fmt.Printf("Warning: failed to mark work as merged: %v\n", err)
			}
		}
	}

	// Mark as having unseen changes
	if err := database.SetWorkHasUnseenPRChanges(ctx, work.ID, true); err != nil {
		if !quiet {
			fmt.Printf("Warning: failed to set unseen PR changes: %v\n", err)
		}
	}

	return true
}

// stringSlicesEqual compares two string slices for equality (order-independent)
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for comparison
	aMap := make(map[string]int)
	for _, s := range a {
		aMap[s]++
	}

	for _, s := range b {
		if aMap[s] == 0 {
			return false
		}
		aMap[s]--
	}

	return true
}
