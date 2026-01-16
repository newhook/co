package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/newhook/co/internal/project"
	"github.com/stretchr/testify/assert"
)

// TestDetectCommandsBarButton tests the Plan mode commands bar button detection
func TestDetectCommandsBarButton(t *testing.T) {
	// Test with both Plan and Resume modes
	testCases := []struct {
		name             string
		hasBeads         bool
		hasActiveSession bool
		pActionText      string
	}{
		{
			name:             "Plan mode",
			hasBeads:         true,
			hasActiveSession: false,
			pActionText:      "[p]Plan",
		},
		{
			name:             "Resume mode",
			hasBeads:         true,
			hasActiveSession: true,
			pActionText:      "[p]Resume",
		},
		{
			name:             "No beads",
			hasBeads:         false,
			hasActiveSession: false,
			pActionText:      "[p]Plan",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create model
			m := &planModel{
				activeBeadSessions: make(map[string]bool),
			}

			if tc.hasBeads {
				m.beadItems = []beadItem{
					{id: "test-bead-1", title: "Test Bead"},
				}
				m.beadsCursor = 0

				if tc.hasActiveSession {
					m.activeBeadSessions["test-bead-1"] = true
				}
			}

			// Build the expected command bar (same logic as detectCommandsBarButton)
			commandsPlain := "[n]New [e]Edit [a]Child [x]Close [A]ssign [i]Import " + tc.pActionText + " [?]Help"

			// Define buttons to test
			buttons := []struct {
				text string
				key  string
			}{
				{"[n]New", "n"},
				{"[e]Edit", "e"},
				{"[a]Child", "a"},
				{"[x]Close", "x"},
				{"[A]ssign", "A"},
				{"[i]Import", "i"},
				{tc.pActionText, "p"},
				{"[?]Help", "?"},
			}

			// Test each button
			for _, btn := range buttons {
				btnStart := strings.Index(commandsPlain, btn.text)
				btnWidth := len(btn.text)

				// Account for padding adjustment in the function (x = x - 1)
				// The function checks: if x >= btnStart && x < btnStart+btnWidth (after adjustment)
				// So valid adjusted range is [btnStart, btnStart+btnWidth)
				// Which means valid mouse X is [btnStart+1, btnStart+btnWidth+1)
				actualStart := btnStart + 1
				actualEnd := btnStart + btnWidth + 1 // first invalid position (after the button)

				t.Run(btn.text+" start", func(t *testing.T) {
					result := m.detectCommandsBarButton(actualStart)
					assert.Equal(t, btn.key, result, "Start of %s at position %d", btn.text, actualStart)
				})

				t.Run(btn.text+" middle", func(t *testing.T) {
					middlePos := actualStart + btnWidth/2
					result := m.detectCommandsBarButton(middlePos)
					assert.Equal(t, btn.key, result, "Middle of %s at position %d", btn.text, middlePos)
				})

				t.Run(btn.text+" end", func(t *testing.T) {
					// Test last valid character
					result := m.detectCommandsBarButton(actualEnd - 1)
					assert.Equal(t, btn.key, result, "End of %s at position %d", btn.text, actualEnd-1)
				})

				t.Run(btn.text+" after", func(t *testing.T) {
					// Test first invalid position after button
					result := m.detectCommandsBarButton(actualEnd)
					assert.NotEqual(t, btn.key, result, "After %s at position %d", btn.text, actualEnd)
				})
			}

			// Test edge cases
			t.Run("before padding", func(t *testing.T) {
				result := m.detectCommandsBarButton(0)
				assert.Equal(t, "", result, "Position 0 should return empty")
			})

			t.Run("negative position", func(t *testing.T) {
				result := m.detectCommandsBarButton(-5)
				assert.Equal(t, "", result, "Negative position should return empty")
			})

			t.Run("far beyond bar", func(t *testing.T) {
				result := m.detectCommandsBarButton(200)
				assert.Equal(t, "", result, "Far position should return empty")
			})
		})
	}
}

// TestDetectHoveredMode tests the Root mode tab detection
func TestDetectHoveredMode(t *testing.T) {
	// Create model
	ctx := context.Background()
	proj := &project.Project{}
	m := newRootModel(ctx, proj)
	m.activeMode = ModePlan

	// Get the tab bar text (same as renderTabBar produces)
	tabBar := m.renderTabBar()

	// Find each mode hotkey
	modes := []struct {
		text string
		mode Mode
	}{
		{"[P]lan", ModePlan},
		{"[W]ork", ModeWork},
	}

	for _, modeInfo := range modes {
		modeIdx := strings.Index(tabBar, modeInfo.text)

		if modeIdx < 0 {
			t.Fatalf("Could not find %s in tab bar: %q", modeInfo.text, tabBar)
		}

		modeWidth := len(modeInfo.text)

		t.Run(modeInfo.text+" start", func(t *testing.T) {
			result := m.detectHoveredMode(modeIdx)
			assert.Equal(t, modeInfo.mode, result, "Start of %s at position %d", modeInfo.text, modeIdx)
		})

		t.Run(modeInfo.text+" middle", func(t *testing.T) {
			middlePos := modeIdx + modeWidth/2
			result := m.detectHoveredMode(middlePos)
			assert.Equal(t, modeInfo.mode, result, "Middle of %s at position %d", modeInfo.text, middlePos)
		})

		t.Run(modeInfo.text+" end", func(t *testing.T) {
			// Test last character (exclusive boundary)
			result := m.detectHoveredMode(modeIdx + modeWidth - 1)
			assert.Equal(t, modeInfo.mode, result, "End of %s at position %d", modeInfo.text, modeIdx+modeWidth-1)
		})

		t.Run(modeInfo.text+" after", func(t *testing.T) {
			// Test just after the mode hotkey (should not match this mode)
			afterPos := modeIdx + modeWidth
			result := m.detectHoveredMode(afterPos)
			assert.NotEqual(t, modeInfo.mode, result, "After %s at position %d", modeInfo.text, afterPos)
		})
	}

	// Test edge cases
	t.Run("before tabs", func(t *testing.T) {
		result := m.detectHoveredMode(0)
		assert.Equal(t, Mode(-1), result, "Position 0 should return -1")
	})

	t.Run("negative position", func(t *testing.T) {
		result := m.detectHoveredMode(-5)
		assert.Equal(t, Mode(-1), result, "Negative position should return -1")
	})

	t.Run("far beyond bar", func(t *testing.T) {
		result := m.detectHoveredMode(200)
		assert.Equal(t, Mode(-1), result, "Far position should return -1")
	})
}
