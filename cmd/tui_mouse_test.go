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
			commandsPlain := "[n]New [e]Edit [a]Child [x]Close [w]Work " + tc.pActionText + " [?]Help"

			// Define buttons to test
			buttons := []struct {
				text string
				key  string
			}{
				{"[n]New", "n"},
				{"[e]Edit", "e"},
				{"[a]Child", "a"},
				{"[x]Close", "x"},
				{"[w]Work", "w"},
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

// TestDetectStatusBarButton tests the Monitor mode status bar button detection
func TestDetectStatusBarButton(t *testing.T) {
	// Create model
	m := &monitorModel{
		width:  80,
		height: 24,
	}

	// Get the plain status bar text (same as the function uses)
	statusBar := m.renderStatusBarPlain()

	// Find the refresh button
	refreshText := "[r]efresh"
	refreshIdx := strings.Index(statusBar, refreshText)

	if refreshIdx < 0 {
		t.Fatal("Could not find [r]efresh in status bar")
	}

	refreshWidth := len(refreshText)

	// Account for padding adjustment (x = x - 1)
	// Valid mouse X range is [refreshIdx+1, refreshIdx+refreshWidth+1)
	actualStart := refreshIdx + 1
	actualEnd := refreshIdx + refreshWidth + 1 // first invalid position

	t.Run("r button start", func(t *testing.T) {
		result := m.detectStatusBarButton(actualStart)
		assert.Equal(t, "r", result, "Start of [r]efresh at position %d", actualStart)
	})

	t.Run("r button middle", func(t *testing.T) {
		middlePos := actualStart + refreshWidth/2
		result := m.detectStatusBarButton(middlePos)
		assert.Equal(t, "r", result, "Middle of [r]efresh at position %d", middlePos)
	})

	t.Run("r button end", func(t *testing.T) {
		result := m.detectStatusBarButton(actualEnd - 1)
		assert.Equal(t, "r", result, "End of [r]efresh at position %d", actualEnd-1)
	})

	t.Run("after r button", func(t *testing.T) {
		result := m.detectStatusBarButton(actualEnd)
		assert.NotEqual(t, "r", result, "After [r]efresh at position %d", actualEnd)
	})

	// Test edge cases
	t.Run("before padding", func(t *testing.T) {
		result := m.detectStatusBarButton(0)
		assert.Equal(t, "", result, "Position 0 should return empty")
	})

	t.Run("negative position", func(t *testing.T) {
		result := m.detectStatusBarButton(-5)
		assert.Equal(t, "", result, "Negative position should return empty")
	})

	t.Run("far beyond bar", func(t *testing.T) {
		result := m.detectStatusBarButton(200)
		assert.Equal(t, "", result, "Far position should return empty")
	})

	t.Run("navigation arrows not clickable", func(t *testing.T) {
		// Try clicking in the navigation area (before refresh button)
		if refreshIdx > 0 {
			result := m.detectStatusBarButton(refreshIdx / 2)
			assert.Equal(t, "", result, "Navigation area should not be clickable")
		}
	})
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
		{"c-[P]lan", ModePlan},
		{"c-[W]ork", ModeWork},
		{"c-[M]onitor", ModeMonitor},
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
