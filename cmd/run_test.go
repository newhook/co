package cmd

import (
	"testing"

	"github.com/newhook/co/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestGroupBeadsByWorkBeadGroup_SingleUngrouped(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 0},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	assert.Equal(t, [][]string{{"bead-1"}}, result)
}

func TestGroupBeadsByWorkBeadGroup_MultipleUngrouped(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 0},
		{BeadID: "bead-2", GroupID: 0},
		{BeadID: "bead-3", GroupID: 0},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// Each ungrouped bead becomes its own task
	assert.Len(t, result, 3)
	assert.Contains(t, result, []string{"bead-1"})
	assert.Contains(t, result, []string{"bead-2"})
	assert.Contains(t, result, []string{"bead-3"})
}

func TestGroupBeadsByWorkBeadGroup_SingleGroup(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 1},
		{BeadID: "bead-2", GroupID: 1},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// Both beads in the same group become one task
	assert.Len(t, result, 1)
	assert.ElementsMatch(t, []string{"bead-1", "bead-2"}, result[0])
}

func TestGroupBeadsByWorkBeadGroup_MultipleGroups(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 1},
		{BeadID: "bead-2", GroupID: 1},
		{BeadID: "bead-3", GroupID: 2},
		{BeadID: "bead-4", GroupID: 2},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// Two groups = two tasks
	assert.Len(t, result, 2)
}

func TestGroupBeadsByWorkBeadGroup_MixedGroupedAndUngrouped(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 0},
		{BeadID: "bead-2", GroupID: 1},
		{BeadID: "bead-3", GroupID: 1},
		{BeadID: "bead-4", GroupID: 0},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// 2 ungrouped (each their own task) + 1 group = 3 tasks
	assert.Len(t, result, 3)

	// Find the grouped result
	var groupedBeads []string
	for _, group := range result {
		if len(group) == 2 {
			groupedBeads = group
		}
	}
	assert.ElementsMatch(t, []string{"bead-2", "bead-3"}, groupedBeads)
}

func TestGroupBeadsByWorkBeadGroup_Empty(t *testing.T) {
	result := groupBeadsByWorkBeadGroup([]*db.WorkBead{})

	assert.Empty(t, result)
}

func TestGroupBeadsByWorkBeadGroup_Nil(t *testing.T) {
	result := groupBeadsByWorkBeadGroup(nil)

	assert.Empty(t, result)
}

func TestGroupBeadsByWorkBeadGroup_UngroupedFirst(t *testing.T) {
	// Verify that ungrouped beads (group_id = 0) come before grouped beads
	workBeads := []*db.WorkBead{
		{BeadID: "grouped-1", GroupID: 1},
		{BeadID: "ungrouped-1", GroupID: 0},
		{BeadID: "grouped-2", GroupID: 1},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// First task should be the ungrouped bead
	assert.Len(t, result, 2)
	assert.Equal(t, []string{"ungrouped-1"}, result[0])
}

func TestGroupBeadsByWorkBeadGroup_LargeGroupID(t *testing.T) {
	// Test with a large group ID to ensure no issues with int64
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 999999999},
		{BeadID: "bead-2", GroupID: 999999999},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	assert.Len(t, result, 1)
	assert.ElementsMatch(t, []string{"bead-1", "bead-2"}, result[0])
}

func TestGroupBeadsByWorkBeadGroup_SingleBeadInGroup(t *testing.T) {
	// A single bead with a non-zero group ID still forms a group
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 5},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	assert.Equal(t, [][]string{{"bead-1"}}, result)
}

func TestGroupBeadsByWorkBeadGroup_PreservesOrder(t *testing.T) {
	// Ungrouped beads should maintain their order
	workBeads := []*db.WorkBead{
		{BeadID: "bead-c", GroupID: 0},
		{BeadID: "bead-a", GroupID: 0},
		{BeadID: "bead-b", GroupID: 0},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// Each ungrouped bead becomes its own task in order
	assert.Len(t, result, 3)
	assert.Equal(t, "bead-c", result[0][0])
	assert.Equal(t, "bead-a", result[1][0])
	assert.Equal(t, "bead-b", result[2][0])
}

func TestGroupBeadsByWorkBeadGroup_ManyGroups(t *testing.T) {
	// Test with many different groups
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1", GroupID: 10},
		{BeadID: "bead-2", GroupID: 20},
		{BeadID: "bead-3", GroupID: 10},
		{BeadID: "bead-4", GroupID: 30},
		{BeadID: "bead-5", GroupID: 20},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// 3 groups (10, 20, 30)
	assert.Len(t, result, 3)

	// Verify each group has correct beads
	groupContents := make(map[int][]string)
	for _, group := range result {
		groupContents[len(group)] = append(groupContents[len(group)], group...)
	}

	// 2 groups with 2 beads, 1 group with 1 bead
	assert.Len(t, groupContents[2], 4, "Expected 2 groups with 2 beads each")
	assert.Len(t, groupContents[1], 1, "Expected 1 group with 1 bead")
}
