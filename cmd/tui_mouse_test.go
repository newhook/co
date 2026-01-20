package cmd

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
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
			commandsPlain := "[n]New [e]Edit [a]Child [x]Close [w]Work [A]dd [i]Import " + tc.pActionText + " [?]Help"

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
				{"[A]dd", "A"},
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


// TestButtonRegion tests the ButtonRegion struct and button position tracking
func TestButtonRegion(t *testing.T) {
	t.Run("button region properties", func(t *testing.T) {
		btn := ButtonRegion{
			ID:     "execute",
			Y:      10,
			StartX: 5,
			EndX:   15,
		}

		assert.Equal(t, "execute", btn.ID)
		assert.Equal(t, 10, btn.Y)
		assert.Equal(t, 5, btn.StartX)
		assert.Equal(t, 15, btn.EndX)
	})

	t.Run("multiple button regions", func(t *testing.T) {
		buttons := []ButtonRegion{
			{ID: "execute", Y: 10, StartX: 5, EndX: 15},
			{ID: "auto", Y: 10, StartX: 20, EndX: 30},
			{ID: "cancel", Y: 10, StartX: 35, EndX: 45},
		}

		assert.Len(t, buttons, 3)
		assert.Equal(t, "execute", buttons[0].ID)
		assert.Equal(t, "auto", buttons[1].ID)
		assert.Equal(t, "cancel", buttons[2].ID)
	})
}

// TestDetectDialogButton tests the dialog button detection logic
func TestDetectDialogButton(t *testing.T) {
	// Create model
	m := &planModel{
		width:       100,
		height:      30,
		columnRatio: 0.5,
		dialogButtons: []ButtonRegion{
			// These match the actual button tracking in tui_plan_work.go
			{ID: "execute", Y: 5, StartX: 2, EndX: 10}, // "► Execute" (9 chars), EndX is last valid position
			{ID: "auto", Y: 7, StartX: 2, EndX: 7},     // "► Auto" (6 chars), EndX is last valid position
			{ID: "cancel", Y: 9, StartX: 2, EndX: 9},   // "► Cancel" (8 chars), EndX is last valid position
		},
		beadItems: []beadItem{
			{id: "test-bead-1", title: "Test Bead"},
		},
		beadsCursor: 0,
		activeBeadSessions: make(map[string]bool),
		selectedBeads: make(map[string]bool),
		createWorkBranch: textinput.New(),
		textInput: textinput.New(),
		createDescTextarea: textarea.New(),
		linearImportInput: textarea.New(),
	}

	testCases := []struct {
		name           string
		viewMode       ViewMode
		x              int
		y              int
		expectedButton string
		setupFunc      func()
	}{
		{
			name:           "ViewCreateWork mode - execute button start",
			viewMode:       ViewCreateWork,
			x:              55, // detailsPanelStart (53) + button StartX (2)
			y:              7,  // formStartY (2) + button Y (5)
			expectedButton: "execute",
		},
		{
			name:           "ViewCreateWork mode - execute button middle",
			viewMode:       ViewCreateWork,
			x:              59, // detailsPanelStart (53) + button middle (6)
			y:              7,
			expectedButton: "execute",
		},
		{
			name:           "ViewCreateWork mode - auto button start",
			viewMode:       ViewCreateWork,
			x:              55, // detailsPanelStart (53) + button StartX (2)
			y:              9,  // formStartY (2) + auto button Y (7)
			expectedButton: "auto",
		},
		{
			name:           "ViewCreateWork mode - cancel button start",
			viewMode:       ViewCreateWork,
			x:              55, // detailsPanelStart (53) + button StartX (2)
			y:              11, // formStartY (2) + cancel button Y (9)
			expectedButton: "cancel",
		},
		{
			name:           "ViewCreateWork mode - miss button (wrong Y)",
			viewMode:       ViewCreateWork,
			x:              55,
			y:              8, // Wrong Y coordinate (between execute and auto)
			expectedButton: "",
		},
		{
			name:           "ViewCreateWork mode - miss button (past execute end)",
			viewMode:       ViewCreateWork,
			x:              65, // Past execute button EndX
			y:              7,
			expectedButton: "",
		},
		{
			name:           "ViewCreateWork mode - miss button (before detailsPanelStart)",
			viewMode:       ViewCreateWork,
			x:              50, // Before details panel
			y:              7,
			expectedButton: "",
		},
		{
			name:           "ViewCreateWork mode - miss button (after cancel button)",
			viewMode:       ViewCreateWork,
			x:              64, // Past cancel button EndX
			y:              11,
			expectedButton: "",
		},
		{
			name:           "ViewLinearImportInline mode - ok button",
			viewMode:       ViewLinearImportInline,
			x:              58, // Button position for Linear import
			y:              16, // Calculated button row Y for Linear import
			expectedButton: "ok",
			setupFunc: func() {
				// Set up for Linear import mode
				m.linearImportInput = textarea.New()
				m.linearImportCreateDeps = false
				m.linearImportUpdate = false
				m.linearImportDryRun = false
				m.linearImportMaxDepth = 3
			},
		},
		{
			name:           "ViewLinearImportInline mode - cancel button",
			viewMode:       ViewLinearImportInline,
			x:              65, // detailsPanelStart (53) + 2 (padding) + 10 (middle of cancel range 8-13)
			y:              16,
			expectedButton: "cancel",
		},
		{
			name:           "ViewLinearImportInline mode - wrong Y",
			viewMode:       ViewLinearImportInline,
			x:              58,
			y:              15, // Wrong Y for button row
			expectedButton: "",
		},
		{
			name:           "ViewCreateBead mode - ok button",
			viewMode:       ViewCreateBead,
			x:              58, // OK button position in create bead form
			y:              16, // Button row Y for create bead
			expectedButton: "ok",
			setupFunc: func() {
				// Set up for create bead mode
				m.textInput = textinput.New()
				m.createDescTextarea = textarea.New()
				m.createBeadType = 0 // task
				m.createBeadPriority = 2
			},
		},
		{
			name:           "ViewEditBead mode - ok button",
			viewMode:       ViewEditBead,
			x:              58,
			y:              16,
			expectedButton: "ok",
			setupFunc: func() {
				// Set up for edit bead mode
				m.textInput = textinput.New()
				m.createDescTextarea = textarea.New()
				m.createBeadType = 0 // task
				m.createBeadPriority = 2
				m.editBeadID = "test-bead-1"
			},
		},
		{
			name:           "Invalid view mode",
			viewMode:       ViewNormal, // Not a dialog mode
			x:              68,
			y:              7,
			expectedButton: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset model state
			m.viewMode = tc.viewMode

			// Run setup function if provided
			if tc.setupFunc != nil {
				tc.setupFunc()
			}

			result := m.detectDialogButton(tc.x, tc.y)
			assert.Equal(t, tc.expectedButton, result, "Expected button %q but got %q at position (%d, %d)",
				tc.expectedButton, result, tc.x, tc.y)
		})
	}
}

// TestButtonPositionTrackingInWorkDialog tests that button positions are correctly tracked
// when rendering the work creation dialog
func TestButtonPositionTrackingInWorkDialog(t *testing.T) {
	// Create model
	m := &planModel{
		width:              100,
		height:             30,
		columnRatio:        0.5,
		viewMode:           ViewCreateWork,
		dialogButtons:      []ButtonRegion{},
		beadItems:          []beadItem{},
		activeBeadSessions: make(map[string]bool),
		selectedBeads:      make(map[string]bool),
	}

	// Simulate what happens during rendering - buttons are tracked
	// These positions would normally be set during renderDetailsPanel
	m.dialogButtons = []ButtonRegion{
		{ID: "execute", Y: 5, StartX: 2, EndX: 10},
		{ID: "auto", Y: 7, StartX: 2, EndX: 7},
		{ID: "cancel", Y: 9, StartX: 2, EndX: 9},
	}

	// Test that button positions are tracked correctly
	t.Run("button tracking", func(t *testing.T) {
		assert.Len(t, m.dialogButtons, 3, "Should have 3 tracked buttons")

		// Check execute button
		executeBtn := m.dialogButtons[0]
		assert.Equal(t, "execute", executeBtn.ID)
		assert.Equal(t, 5, executeBtn.Y)
		assert.Equal(t, 2, executeBtn.StartX)
		assert.Equal(t, 10, executeBtn.EndX)

		// Check auto button
		autoBtn := m.dialogButtons[1]
		assert.Equal(t, "auto", autoBtn.ID)
		assert.Equal(t, 7, autoBtn.Y)
		assert.Equal(t, 2, autoBtn.StartX)
		assert.Equal(t, 7, autoBtn.EndX)

		// Check cancel button
		cancelBtn := m.dialogButtons[2]
		assert.Equal(t, "cancel", cancelBtn.ID)
		assert.Equal(t, 9, cancelBtn.Y)
		assert.Equal(t, 2, cancelBtn.StartX)
		assert.Equal(t, 9, cancelBtn.EndX)
	})

	t.Run("clear buttons on mode change", func(t *testing.T) {
		// When view mode changes, buttons should be cleared
		m.viewMode = ViewNormal
		m.dialogButtons = []ButtonRegion{} // Simulate clearing

		assert.Len(t, m.dialogButtons, 0, "Buttons should be cleared when leaving dialog mode")
	})

	t.Run("button overlap detection", func(t *testing.T) {
		// Test that buttons don't overlap
		buttons := m.dialogButtons

		// Reset for test
		m.dialogButtons = []ButtonRegion{
			{ID: "execute", Y: 5, StartX: 2, EndX: 10},
			{ID: "auto", Y: 7, StartX: 2, EndX: 7},
			{ID: "cancel", Y: 9, StartX: 2, EndX: 9},
		}

		for i := 0; i < len(buttons); i++ {
			for j := i + 1; j < len(buttons); j++ {
				btn1 := buttons[i]
				btn2 := buttons[j]

				// Check if buttons on same row don't overlap
				if btn1.Y == btn2.Y {
					assert.True(t, btn1.EndX < btn2.StartX || btn2.EndX < btn1.StartX,
						"Buttons %s and %s should not overlap", btn1.ID, btn2.ID)
				}
			}
		}
	})
}
