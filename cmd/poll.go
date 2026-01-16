package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

var (
	flagPollInterval time.Duration
	flagPollProject  string
	flagPollWork     string
)

var pollCmd = &cobra.Command{
	Use:   "poll [work-id|task-id]",
	Short: "Monitor work/task progress with text output",
	Long: `Poll monitors the progress of works and tasks with simple text output.

Without arguments:
- If in a work directory or --work specified: monitors that work's tasks
- Otherwise: monitors all active works in the project

With an ID:
- If ID contains a dot (e.g., w-xxx.1): monitors that specific task
- If ID is a work ID (e.g., w-xxx): monitors all tasks in that work

Output shows:
- Work status and branch
- Task progress counts
- Status indicators (○ pending, ● processing, ✓ completed, ✗ failed)

For interactive management (create, destroy, plan, run works),
use 'co tui' instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPoll,
}

func init() {
	rootCmd.AddCommand(pollCmd)
	pollCmd.Flags().DurationVarP(&flagPollInterval, "interval", "i", 2*time.Second, "polling interval")
	pollCmd.Flags().StringVar(&flagPollProject, "project", "", "project directory (default: auto-detect)")
	pollCmd.Flags().StringVar(&flagPollWork, "work", "", "work ID to monitor (default: auto-detect)")
}

// workProgress holds progress info for a work unit (used by tui.go)
type workProgress struct {
	work                *db.Work
	tasks               []*taskProgress
	workBeads           []beadProgress // all beads assigned to this work
	unassignedBeadCount int
}

// taskProgress holds progress info for a task (used by tui.go)
type taskProgress struct {
	task  *db.Task
	beads []beadProgress
}

// beadProgress holds progress info for a bead (used by tui.go)
type beadProgress struct {
	id          string
	status      string
	title       string
	description string
	beadStatus  string // status from beads (open/closed)
	priority    int
	issueType   string
}

// fetchPollData fetches progress data for works/tasks (used by tui.go)
func fetchPollData(ctx context.Context, proj *project.Project, workID, taskID string) ([]*workProgress, error) {
	// Create beads client for fetching bead details
	beadsDBPath := filepath.Join(proj.MainRepoPath(), ".beads", "beads.db")
	beadsClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create beads client: %w", err)
	}
	defer beadsClient.Close()

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
			bp := beadProgress{id: beadID, status: status}
			// Fetch additional bead details from beads system
			if bead, err := beadsClient.GetBead(ctx, beadID); err == nil && bead != nil {
				bp.title = bead.Title
				bp.description = bead.Description
				bp.beadStatus = bead.Status
			}
			tp.beads = append(tp.beads, bp)
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

		wp, err := fetchWorkProgress(ctx, proj, work, beadsClient)
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

			wp, err := fetchWorkProgress(ctx, proj, work, beadsClient)
			if err != nil {
				continue // Skip works with errors
			}
			works = append(works, wp)
		}
	}

	return works, nil
}

func fetchWorkProgress(ctx context.Context, proj *project.Project, work *db.Work, beadsClient *beads.Client) (*workProgress, error) {
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
			bp := beadProgress{id: beadID, status: status}
			// Fetch additional bead details from beads system
			if bead, err := beadsClient.GetBead(ctx, beadID); err == nil && bead != nil {
				bp.title = bead.Title
				bp.description = bead.Description
				bp.beadStatus = bead.Status
			}
			tp.beads = append(tp.beads, bp)
		}
		wp.tasks = append(wp.tasks, tp)
	}

	// Get all beads for this work
	allWorkBeads, err := proj.DB.GetWorkBeads(ctx, work.ID)
	if err == nil {
		for _, wb := range allWorkBeads {
			bp := beadProgress{id: wb.BeadID}
			// Fetch additional bead details from beads system
			if bead, err := beadsClient.GetBead(ctx, wb.BeadID); err == nil && bead != nil {
				bp.title = bead.Title
				bp.description = bead.Description
				bp.beadStatus = bead.Status
				bp.priority = bead.Priority
				bp.issueType = bead.Type
			}
			wp.workBeads = append(wp.workBeads, bp)
		}
	}

	// Get count of unassigned beads for this work
	unassignedBeads, err := proj.DB.GetUnassignedWorkBeads(ctx, work.ID)
	if err == nil {
		wp.unassignedBeadCount = len(unassignedBeads)
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
		} else if strings.HasPrefix(argID, "w-") {
			// Work ID
			workID = argID
		} else {
			return fmt.Errorf("invalid ID format: %s", argID)
		}
	} else if flagPollWork != "" {
		workID = flagPollWork
	} else {
		// Try to detect from current directory
		// Ignore errors - poll can show all works as fallback
		workID, _ = detectWorkFromDirectory(proj)
	}

	fmt.Println("Monitoring progress...")
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
				printWorkProgress(wp)
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

func printWorkProgress(wp *workProgress) {
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
