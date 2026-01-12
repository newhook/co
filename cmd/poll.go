package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	flagPollQuiet    bool
	flagPollInterval time.Duration
	flagPollProject  string
	flagPollWork     string
)

var pollCmd = &cobra.Command{
	Use:   "poll [work-id|task-id]",
	Short: "Monitor work/task progress with live updates",
	Long: `Poll monitors the progress of works and tasks with a beautiful TUI.

Without arguments:
- If in a work directory or --work specified: monitors that work's tasks
- Otherwise: monitors all active works in the project

With an ID:
- If ID contains a dot (e.g., w-xxx.1): monitors that specific task
- If ID is a work ID (e.g., w-xxx): monitors all tasks in that work

The TUI shows:
- Overall progress with progress bars
- Task status with spinners for active tasks
- Color-coded status indicators
- Live updates at configurable intervals

Use --quiet for simple text output without the TUI.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPoll,
}

func init() {
	rootCmd.AddCommand(pollCmd)
	pollCmd.Flags().BoolVarP(&flagPollQuiet, "quiet", "q", false, "simple text output without TUI")
	pollCmd.Flags().DurationVarP(&flagPollInterval, "interval", "i", 2*time.Second, "polling interval")
	pollCmd.Flags().StringVar(&flagPollProject, "project", "", "project directory (default: auto-detect)")
	pollCmd.Flags().StringVar(&flagPollWork, "work", "", "work ID to monitor (default: auto-detect)")
}

// Style definitions
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))

	statusPending = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	statusProcessing = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Bold(true)

	statusCompleted = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	statusFailed = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	progressBarFull = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	progressBarEmpty = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
)

// Messages for Bubble Tea
type tickMsg time.Time
type pollDataMsg struct {
	works []*workProgress
	err   error
}

type workProgress struct {
	work  *db.Work
	tasks []*taskProgress
}

type taskProgress struct {
	task  *db.Task
	beads []beadProgress
}

type beadProgress struct {
	id     string
	status string
}

// Model for Bubble Tea
type pollModel struct {
	ctx           context.Context
	proj          *project.Project
	workID        string
	taskID        string
	spinner       spinner.Model
	works         []*workProgress
	err           error
	interval      time.Duration
	width         int
	height        int
	lastUpdate    time.Time
	quitting      bool
}

func newPollModel(ctx context.Context, proj *project.Project, workID, taskID string, interval time.Duration) pollModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	return pollModel{
		ctx:      ctx,
		proj:     proj,
		workID:   workID,
		taskID:   taskID,
		spinner:  s,
		interval: interval,
		width:    80,
		height:   24,
	}
}

func (m pollModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.pollData(),
		m.tick(),
	)
}

func (m pollModel) tick() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m pollModel) pollData() tea.Cmd {
	return func() tea.Msg {
		works, err := fetchPollData(m.ctx, m.proj, m.workID, m.taskID)
		return pollDataMsg{works: works, err: err}
	}
}

func (m pollModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.pollData(), m.tick())

	case pollDataMsg:
		m.works = msg.works
		m.err = msg.err
		m.lastUpdate = time.Now()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m pollModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("Claude Orchestrator - Progress Monitor"))
	b.WriteString("\n\n")

	// Error display
	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	// No data yet
	if len(m.works) == 0 && m.err == nil {
		b.WriteString(m.spinner.View())
		b.WriteString(" Loading...")
		b.WriteString("\n")
		return b.String()
	}

	// Render each work
	for _, wp := range m.works {
		b.WriteString(m.renderWork(wp))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(dimStyle.Render(fmt.Sprintf("\nLast update: %s | Press 'q' to quit",
		m.lastUpdate.Format("15:04:05"))))
	b.WriteString("\n")

	return b.String()
}

func (m pollModel) renderWork(wp *workProgress) string {
	var b strings.Builder

	// Work header
	statusIcon := m.statusIcon(wp.work.Status)
	workHeader := fmt.Sprintf("%s Work: %s", statusIcon, wp.work.ID)
	b.WriteString(headerStyle.Render(workHeader))
	b.WriteString("\n")

	// Work details
	b.WriteString(labelStyle.Render("  Branch: "))
	b.WriteString(valueStyle.Render(wp.work.BranchName))
	b.WriteString("\n")

	if wp.work.PRURL != "" {
		b.WriteString(labelStyle.Render("  PR: "))
		b.WriteString(valueStyle.Render(wp.work.PRURL))
		b.WriteString("\n")
	}

	// Progress bar
	completed := 0
	processing := 0
	total := len(wp.tasks)
	for _, tp := range wp.tasks {
		switch tp.task.Status {
		case db.StatusCompleted:
			completed++
		case db.StatusProcessing:
			processing++
		}
	}

	b.WriteString(labelStyle.Render("  Progress: "))
	b.WriteString(m.renderProgressBar(completed, total, 30))
	b.WriteString(fmt.Sprintf(" %d/%d", completed, total))
	b.WriteString("\n\n")

	// Tasks
	b.WriteString(labelStyle.Render("  Tasks:"))
	b.WriteString("\n")

	for _, tp := range wp.tasks {
		b.WriteString(m.renderTask(tp))
	}

	return b.String()
}

func (m pollModel) renderTask(tp *taskProgress) string {
	var b strings.Builder

	// Task status icon and spinner
	var statusDisplay string
	switch tp.task.Status {
	case db.StatusPending:
		statusDisplay = statusPending.Render("○")
	case db.StatusProcessing:
		statusDisplay = m.spinner.View()
	case db.StatusCompleted:
		statusDisplay = statusCompleted.Render("✓")
	case db.StatusFailed:
		statusDisplay = statusFailed.Render("✗")
	default:
		statusDisplay = "?"
	}

	taskType := tp.task.TaskType
	if taskType == "" {
		taskType = "implement"
	}

	b.WriteString(fmt.Sprintf("    %s %s [%s]",
		statusDisplay,
		tp.task.ID,
		dimStyle.Render(taskType)))
	b.WriteString("\n")

	// Beads for this task
	for _, bp := range tp.beads {
		beadStatus := m.statusIcon(bp.status)
		b.WriteString(fmt.Sprintf("      %s %s\n", beadStatus, bp.id))
	}

	// Error message if failed
	if tp.task.Status == db.StatusFailed && tp.task.ErrorMessage != "" {
		errMsg := tp.task.ErrorMessage
		if len(errMsg) > 50 {
			errMsg = errMsg[:47] + "..."
		}
		b.WriteString(errorStyle.Render(fmt.Sprintf("      Error: %s\n", errMsg)))
	}

	return b.String()
}

func (m pollModel) statusIcon(status string) string {
	switch status {
	case db.StatusPending:
		return statusPending.Render("○")
	case db.StatusProcessing:
		return statusProcessing.Render("●")
	case db.StatusCompleted:
		return statusCompleted.Render("✓")
	case db.StatusFailed:
		return statusFailed.Render("✗")
	default:
		return "?"
	}
}

func (m pollModel) renderProgressBar(completed, total, width int) string {
	if total == 0 {
		return progressBarEmpty.Render(strings.Repeat("─", width))
	}

	filled := min(int(float64(completed)/float64(total)*float64(width)), width)

	bar := progressBarFull.Render(strings.Repeat("█", filled))
	bar += progressBarEmpty.Render(strings.Repeat("░", width-filled))

	return bar
}

func fetchPollData(ctx context.Context, proj *project.Project, workID, taskID string) ([]*workProgress, error) {
	var works []*workProgress

	if taskID != "" {
		// Single task mode
		task, err := proj.DB.GetTask(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("failed to get task: %w", err)
		}
		if task == nil {
			return nil, fmt.Errorf("task %s not found", taskID)
		}

		// Get the work for this task
		work, err := proj.DB.GetWork(ctx, task.WorkID)
		if err != nil {
			return nil, fmt.Errorf("failed to get work: %w", err)
		}
		if work == nil {
			work = &db.Work{ID: task.WorkID, Status: "unknown"}
		}

		tp := &taskProgress{task: task}
		beadIDs, _ := proj.DB.GetTaskBeads(ctx, taskID)
		for _, beadID := range beadIDs {
			status, _ := proj.DB.GetTaskBeadStatus(ctx, taskID, beadID)
			if status == "" {
				status = db.StatusPending
			}
			tp.beads = append(tp.beads, beadProgress{id: beadID, status: status})
		}

		works = append(works, &workProgress{
			work:  work,
			tasks: []*taskProgress{tp},
		})
	} else if workID != "" {
		// Single work mode
		work, err := proj.DB.GetWork(ctx, workID)
		if err != nil {
			return nil, fmt.Errorf("failed to get work: %w", err)
		}
		if work == nil {
			return nil, fmt.Errorf("work %s not found", workID)
		}

		wp, err := fetchWorkProgress(ctx, proj, work)
		if err != nil {
			return nil, err
		}
		works = append(works, wp)
	} else {
		// All active works mode
		allWorks, err := proj.DB.ListWorks(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("failed to list works: %w", err)
		}

		for _, work := range allWorks {
			// Only show active works (pending or processing)
			if work.Status == db.StatusCompleted {
				continue
			}

			wp, err := fetchWorkProgress(ctx, proj, work)
			if err != nil {
				continue // Skip works with errors
			}
			works = append(works, wp)
		}
	}

	return works, nil
}

func fetchWorkProgress(ctx context.Context, proj *project.Project, work *db.Work) (*workProgress, error) {
	wp := &workProgress{work: work}

	tasks, err := proj.DB.GetWorkTasks(ctx, work.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	for _, task := range tasks {
		tp := &taskProgress{task: task}
		beadIDs, _ := proj.DB.GetTaskBeads(ctx, task.ID)
		for _, beadID := range beadIDs {
			status, _ := proj.DB.GetTaskBeadStatus(ctx, task.ID, beadID)
			if status == "" {
				status = db.StatusPending
			}
			tp.beads = append(tp.beads, beadProgress{id: beadID, status: status})
		}
		wp.tasks = append(wp.tasks, tp)
	}

	return wp, nil
}

func runPoll(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, flagPollProject)
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Determine what to monitor
	var workID, taskID string

	if len(args) > 0 {
		argID := args[0]
		if strings.Contains(argID, ".") {
			// Task ID
			taskID = argID
		} else if strings.HasPrefix(argID, "w-") || strings.HasPrefix(argID, "work-") {
			// Work ID
			workID = argID
		} else {
			return fmt.Errorf("invalid ID format: %s", argID)
		}
	} else if flagPollWork != "" {
		workID = flagPollWork
	} else {
		// Try to detect from current directory
		workID, _ = detectWorkFromDirectory(proj)
	}

	if flagPollQuiet {
		return runPollQuiet(ctx, proj, workID, taskID)
	}

	// Run the TUI
	model := newPollModel(ctx, proj, workID, taskID, flagPollInterval)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}

func runPollQuiet(ctx context.Context, proj *project.Project, workID, taskID string) error {
	fmt.Println("Monitoring progress (quiet mode)...")
	fmt.Println("Press Ctrl+C to exit")
	fmt.Println()

	ticker := time.NewTicker(flagPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			works, err := fetchPollData(ctx, proj, workID, taskID)
			if err != nil {
				fmt.Printf("[%s] Error: %v\n", time.Now().Format("15:04:05"), err)
				continue
			}

			// Clear screen (ANSI escape)
			fmt.Print("\033[2J\033[H")

			fmt.Printf("=== Progress Update [%s] ===\n\n", time.Now().Format("15:04:05"))

			for _, wp := range works {
				printWorkQuiet(wp)
			}

			if len(works) == 0 {
				fmt.Println("No active works found")
			}

			// Check if all work is complete
			allComplete := true
			for _, wp := range works {
				if wp.work.Status != db.StatusCompleted {
					allComplete = false
					break
				}
			}

			if allComplete && len(works) > 0 {
				fmt.Println("\nAll work completed!")
				return nil
			}
		}
	}
}

func printWorkQuiet(wp *workProgress) {
	statusSymbol := "?"
	switch wp.work.Status {
	case db.StatusPending:
		statusSymbol = "○"
	case db.StatusProcessing:
		statusSymbol = "●"
	case db.StatusCompleted:
		statusSymbol = "✓"
	case db.StatusFailed:
		statusSymbol = "✗"
	}

	fmt.Printf("%s Work: %s (%s)\n", statusSymbol, wp.work.ID, wp.work.Status)
	fmt.Printf("  Branch: %s\n", wp.work.BranchName)

	if wp.work.PRURL != "" {
		fmt.Printf("  PR: %s\n", wp.work.PRURL)
	}

	completed := 0
	for _, tp := range wp.tasks {
		if tp.task.Status == db.StatusCompleted {
			completed++
		}
	}
	fmt.Printf("  Progress: %d/%d tasks\n", completed, len(wp.tasks))

	fmt.Println("  Tasks:")
	for _, tp := range wp.tasks {
		taskSymbol := "?"
		switch tp.task.Status {
		case db.StatusPending:
			taskSymbol = "○"
		case db.StatusProcessing:
			taskSymbol = "●"
		case db.StatusCompleted:
			taskSymbol = "✓"
		case db.StatusFailed:
			taskSymbol = "✗"
		}

		taskType := tp.task.TaskType
		if taskType == "" {
			taskType = "implement"
		}

		fmt.Printf("    %s %s [%s]\n", taskSymbol, tp.task.ID, taskType)

		for _, bp := range tp.beads {
			beadSymbol := "○"
			switch bp.status {
			case db.StatusCompleted:
				beadSymbol = "✓"
			case db.StatusProcessing:
				beadSymbol = "●"
			case db.StatusFailed:
				beadSymbol = "✗"
			}
			fmt.Printf("      %s %s\n", beadSymbol, bp.id)
		}

		if tp.task.Status == db.StatusFailed && tp.task.ErrorMessage != "" {
			fmt.Printf("      Error: %s\n", tp.task.ErrorMessage)
		}
	}
	fmt.Println()
}
