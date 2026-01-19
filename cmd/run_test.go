package cmd

import (
	"testing"

	"github.com/newhook/co/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestGroupBeadsByWorkBeadGroup_Single(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1"},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	assert.Equal(t, [][]string{{"bead-1"}}, result)
}

func TestGroupBeadsByWorkBeadGroup_Multiple(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-1"},
		{BeadID: "bead-2"},
		{BeadID: "bead-3"},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// Each bead becomes its own task
	assert.Len(t, result, 3)
	assert.Equal(t, []string{"bead-1"}, result[0])
	assert.Equal(t, []string{"bead-2"}, result[1])
	assert.Equal(t, []string{"bead-3"}, result[2])
}

func TestGroupBeadsByWorkBeadGroup_Empty(t *testing.T) {
	result := groupBeadsByWorkBeadGroup([]*db.WorkBead{})

	assert.Empty(t, result)
}

func TestGroupBeadsByWorkBeadGroup_Nil(t *testing.T) {
	result := groupBeadsByWorkBeadGroup(nil)

	assert.Empty(t, result)
}

func TestGroupBeadsByWorkBeadGroup_PreservesOrder(t *testing.T) {
	workBeads := []*db.WorkBead{
		{BeadID: "bead-c"},
		{BeadID: "bead-a"},
		{BeadID: "bead-b"},
	}

	result := groupBeadsByWorkBeadGroup(workBeads)

	// Each bead becomes its own task in order
	assert.Len(t, result, 3)
	assert.Equal(t, "bead-c", result[0][0])
	assert.Equal(t, "bead-a", result[1][0])
	assert.Equal(t, "bead-b", result[2][0])
}
