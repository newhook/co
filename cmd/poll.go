package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// workProgress holds progress info for a work unit.
type workProgress struct {
	work                *db.Work
	tasks               []*taskProgress
	workBeads           []beadProgress // all beads assigned to this work
	unassignedBeads     []beadProgress // beads in work but not assigned to any task
	unassignedBeadCount int
	feedbackCount       int // count of unresolved PR feedback items
}

// taskProgress holds progress info for a task.
type taskProgress struct {
	task  *db.Task
	beads []beadProgress
}

// beadProgress holds progress info for a bead.
type beadProgress struct {
	id          string
	status      string
	title       string
	description string
	beadStatus  string // status from beads (open/closed)
	priority    int
	issueType   string
}

// fetchTaskPollData fetches progress data for a single task
func fetchTaskPollData(ctx context.Context, proj *project.Project, taskID string) ([]*workProgress, error) {
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

	beadIDs, err := proj.DB.GetTaskBeads(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task beads: %w", err)
	}

	// Batch fetch all bead details
	beadsResult, err := proj.Beads.GetBeadsWithDeps(ctx, beadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get beads: %w", err)
	}

	tp := &taskProgress{task: task}
	for _, beadID := range beadIDs {
		status, err := proj.DB.GetTaskBeadStatus(ctx, taskID, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to get task bead status: %w", err)
		}
		if status == "" {
			status = db.StatusPending
		}
		bp := beadProgress{id: beadID, status: status}
		if bead := beadsResult.GetBead(beadID); bead != nil {
			bp.title = bead.Title
			bp.description = bead.Description
			bp.beadStatus = bead.Status
		}
		tp.beads = append(tp.beads, bp)
	}

	return []*workProgress{{
		work:  work,
		tasks: []*taskProgress{tp},
	}}, nil
}

// fetchWorkPollData fetches progress data for a single work
func fetchWorkPollData(ctx context.Context, proj *project.Project, workID string) ([]*workProgress, error) {
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
	return []*workProgress{wp}, nil
}

// fetchAllWorksPollData fetches progress data for all works
func fetchAllWorksPollData(ctx context.Context, proj *project.Project) ([]*workProgress, error) {
	allWorks, err := proj.DB.ListWorks(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list works: %w", err)
	}

	works := make([]*workProgress, 0, len(allWorks))
	for _, work := range allWorks {
		wp, err := fetchWorkProgress(ctx, proj, work)
		if err != nil {
			continue // Skip works with errors
		}
		works = append(works, wp)
	}
	return works, nil
}

func fetchWorkProgress(ctx context.Context, proj *project.Project, work *db.Work) (*workProgress, error) {
	wp := &workProgress{work: work}

	tasks, err := proj.DB.GetWorkTasks(ctx, work.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks: %w", err)
	}

	// Fetch all task beads for this work in a single query
	allTaskBeads, err := proj.DB.GetTaskBeadsForWork(ctx, work.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task beads: %w", err)
	}

	// Get all work beads
	allWorkBeads, err := proj.DB.GetWorkBeads(ctx, work.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get work beads: %w", err)
	}

	// Get unassigned beads for this work
	unassignedWorkBeads, err := proj.DB.GetUnassignedWorkBeads(ctx, work.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get unassigned beads: %w", err)
	}

	// Collect all bead IDs for batch fetch
	beadIDSet := make(map[string]struct{})
	for _, tb := range allTaskBeads {
		beadIDSet[tb.BeadID] = struct{}{}
	}
	for _, wb := range allWorkBeads {
		beadIDSet[wb.BeadID] = struct{}{}
	}
	for _, wb := range unassignedWorkBeads {
		beadIDSet[wb.BeadID] = struct{}{}
	}
	if work.RootIssueID != "" {
		beadIDSet[work.RootIssueID] = struct{}{}
	}

	beadIDs := make([]string, 0, len(beadIDSet))
	for id := range beadIDSet {
		beadIDs = append(beadIDs, id)
	}

	// Batch fetch all bead details
	beadsResult, err := proj.Beads.GetBeadsWithDeps(ctx, beadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get beads: %w", err)
	}

	// Build a map of task ID -> beads for efficient lookup
	taskBeadsMap := make(map[string][]db.TaskBeadInfo)
	for _, tb := range allTaskBeads {
		taskBeadsMap[tb.TaskID] = append(taskBeadsMap[tb.TaskID], tb)
	}

	for _, task := range tasks {
		tp := &taskProgress{task: task}
		for _, tb := range taskBeadsMap[task.ID] {
			status := tb.Status
			if status == "" {
				status = db.StatusPending
			}
			bp := beadProgress{id: tb.BeadID, status: status}
			if bead := beadsResult.GetBead(tb.BeadID); bead != nil {
				bp.title = bead.Title
				bp.description = bead.Description
				bp.beadStatus = bead.Status
			}
			tp.beads = append(tp.beads, bp)
		}
		wp.tasks = append(wp.tasks, tp)
	}

	// Populate work beads
	for _, wb := range allWorkBeads {
		bp := beadProgress{id: wb.BeadID}
		if bead := beadsResult.GetBead(wb.BeadID); bead != nil {
			bp.title = bead.Title
			bp.description = bead.Description
			bp.beadStatus = bead.Status
			bp.priority = bead.Priority
			bp.issueType = bead.Type
		}
		wp.workBeads = append(wp.workBeads, bp)
	}

	// Ensure root issue is always available for display (it may not be in work_beads if it's an epic)
	if work.RootIssueID != "" {
		rootFound := false
		for _, wb := range wp.workBeads {
			if wb.id == work.RootIssueID {
				rootFound = true
				break
			}
		}
		if !rootFound {
			if rootBead := beadsResult.GetBead(work.RootIssueID); rootBead != nil {
				bp := beadProgress{
					id:          rootBead.ID,
					title:       rootBead.Title,
					description: rootBead.Description,
					beadStatus:  rootBead.Status,
					priority:    rootBead.Priority,
					issueType:   rootBead.Type,
				}
				// Prepend root issue so it appears first
				wp.workBeads = append([]beadProgress{bp}, wp.workBeads...)
			}
		}
	}

	// Populate unassigned beads
	wp.unassignedBeadCount = len(unassignedWorkBeads)
	for _, wb := range unassignedWorkBeads {
		bp := beadProgress{id: wb.BeadID}
		if bead := beadsResult.GetBead(wb.BeadID); bead != nil {
			bp.title = bead.Title
			bp.description = bead.Description
			bp.beadStatus = bead.Status
			bp.priority = bead.Priority
			bp.issueType = bead.Type
		}
		wp.unassignedBeads = append(wp.unassignedBeads, bp)
	}

	// Get feedback count for this work
	feedbackCount, err := proj.DB.CountUnresolvedFeedbackForWork(ctx, work.ID)
	if err == nil {
		wp.feedbackCount = feedbackCount
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
			var works []*workProgress
			var err error
			if taskID != "" {
				works, err = fetchTaskPollData(ctx, proj, taskID)
			} else if workID != "" {
				works, err = fetchWorkPollData(ctx, proj, workID)
			} else {
				works, err = fetchAllWorksPollData(ctx, proj)
			}
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
	if wp.work.RootIssueID != "" {
		fmt.Printf("  Root Issue: %s\n", wp.work.RootIssueID)
	}

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
