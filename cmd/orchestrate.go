package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/project"
	"github.com/spf13/cobra"
)

// Workflow step constants
const (
	StepCreateWork      = 0
	StepCollectBeads    = 1
	StepPlanTasks       = 2
	StepExecuteTasks    = 3
	StepWaitCompletion  = 4
	StepReviewFix       = 5
	StepCreatePR        = 6
	StepCompleted       = 7
)

// StepData holds JSON-serializable data passed between steps
type StepData struct {
	BeadID       string   `json:"bead_id,omitempty"`
	BaseBranch   string   `json:"base_branch,omitempty"`
	WorkID       string   `json:"work_id,omitempty"`
	BranchName   string   `json:"branch_name,omitempty"`
	BeadIDs      []string `json:"bead_ids,omitempty"`
	ReviewCount  int      `json:"review_count,omitempty"`
}

var (
	flagOrchestrateWorkflow string
	flagOrchestrateStep     int
)

var orchestrateCmd = &cobra.Command{
	Use:    "orchestrate",
	Short:  "Execute a single workflow step (internal command)",
	Long:   `Internal command to execute one step of the automated workflow. Used by zellij orchestration.`,
	Hidden: true,
	RunE:   runOrchestrate,
}

func init() {
	orchestrateCmd.Flags().StringVar(&flagOrchestrateWorkflow, "workflow", "", "workflow ID to orchestrate")
	orchestrateCmd.Flags().IntVar(&flagOrchestrateStep, "step", -1, "step number to execute (-1 for auto-detect)")
}

func runOrchestrate(cmd *cobra.Command, args []string) error {
	ctx := GetContext()

	proj, err := project.Find(ctx, "")
	if err != nil {
		return fmt.Errorf("not in a project directory: %w", err)
	}
	defer proj.Close()

	// Get workflow state
	workflowID := flagOrchestrateWorkflow
	if workflowID == "" {
		return fmt.Errorf("--workflow is required")
	}

	state, err := proj.DB.GetWorkflowState(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("no workflow state found for workflow %s", workflowID)
	}

	// Determine which step to execute
	step := flagOrchestrateStep
	if step < 0 {
		step = state.CurrentStep
	}

	// Parse step data
	var stepData StepData
	if state.StepData != "" && state.StepData != "{}" {
		if err := json.Unmarshal([]byte(state.StepData), &stepData); err != nil {
			return fmt.Errorf("failed to parse step data: %w", err)
		}
	}

	fmt.Printf("=== Orchestrating workflow %s, step %d ===\n", workflowID, step)

	// Execute the step
	var nextStep int
	var nextData StepData

	switch step {
	case StepCreateWork:
		nextStep, nextData, err = stepCreateWork(proj, stepData)
	case StepCollectBeads:
		nextStep, nextData, err = stepCollectBeads(proj, stepData)
	case StepPlanTasks:
		nextStep, nextData, err = stepPlanTasks(proj, stepData)
	case StepExecuteTasks:
		nextStep, nextData, err = stepExecuteTasks(proj, stepData)
	case StepWaitCompletion:
		nextStep, nextData, err = stepWaitCompletion(proj, stepData)
	case StepReviewFix:
		nextStep, nextData, err = stepReviewFix(proj, stepData)
	case StepCreatePR:
		nextStep, nextData, err = stepCreatePR(proj, stepData)
	case StepCompleted:
		fmt.Println("Workflow already completed")
		return nil
	default:
		return fmt.Errorf("unknown step: %d", step)
	}

	if err != nil {
		// Mark workflow as failed
		if dbErr := proj.DB.FailWorkflowStep(ctx, workflowID, err.Error()); dbErr != nil {
			fmt.Printf("Warning: failed to update workflow state: %v\n", dbErr)
		}
		return err
	}

	// If StepCreateWork just completed, link the workflow to the new work
	if step == StepCreateWork && nextData.WorkID != "" {
		if err := proj.DB.SetWorkflowWorkID(ctx, workflowID, nextData.WorkID); err != nil {
			return fmt.Errorf("failed to link workflow to work: %w", err)
		}
	}

	// Update workflow state
	nextDataJSON, err := json.Marshal(nextData)
	if err != nil {
		return fmt.Errorf("failed to serialize step data: %w", err)
	}

	if nextStep == StepCompleted {
		if err := proj.DB.CompleteWorkflowStep(ctx, workflowID); err != nil {
			return fmt.Errorf("failed to complete workflow: %w", err)
		}
		fmt.Println("=== Workflow completed successfully ===")
		return nil
	}

	// Update state and spawn next step
	if err := proj.DB.UpdateWorkflowStep(ctx, workflowID, nextStep, "pending", string(nextDataJSON)); err != nil {
		return fmt.Errorf("failed to update workflow state: %w", err)
	}

	// Spawn next step via zellij
	if err := spawnNextStep(proj, workflowID, nextStep); err != nil {
		return fmt.Errorf("failed to spawn next step: %w", err)
	}

	return nil
}

// stepCreateWork creates the work unit with worktree and branch
func stepCreateWork(proj *project.Project, data StepData) (int, StepData, error) {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	if data.BeadID == "" {
		return 0, data, fmt.Errorf("bead_id is required in step data")
	}

	fmt.Printf("Creating work from bead: %s\n", data.BeadID)

	// Get the main bead to generate branch name
	mainBead, err := beads.GetBeadInDir(data.BeadID, mainRepoPath)
	if err != nil {
		return 0, data, fmt.Errorf("failed to get bead %s: %w", data.BeadID, err)
	}

	branchName := generateBranchNameFromBead(mainBead)

	// Ensure branch name is unique (append suffix if needed)
	branchName, err = ensureUniqueBranchName(mainRepoPath, branchName)
	if err != nil {
		return 0, data, fmt.Errorf("failed to find unique branch name: %w", err)
	}
	fmt.Printf("Using branch name: %s\n", branchName)

	// Generate work ID
	workID, err := proj.DB.GenerateWorkID(ctx, branchName, proj.Config.Project.Name)
	if err != nil {
		return 0, data, fmt.Errorf("failed to generate work ID: %w", err)
	}

	// Create work subdirectory
	workDir := filepath.Join(proj.Root, workID)
	if err := os.Mkdir(workDir, 0755); err != nil {
		return 0, data, fmt.Errorf("failed to create work directory: %w", err)
	}

	// Create git worktree inside work directory
	worktreePath := filepath.Join(workDir, "tree")
	baseBranch := data.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	// Create worktree with new branch
	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", branchName, baseBranch)
	cmd.Dir = mainRepoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workDir)
		return 0, data, fmt.Errorf("failed to create worktree: %w\n%s", err, output)
	}

	// Push branch and set upstream
	cmd = exec.Command("git", "push", "--set-upstream", "origin", branchName)
	cmd.Dir = worktreePath
	if output, err := cmd.CombinedOutput(); err != nil {
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return 0, data, fmt.Errorf("failed to push and set upstream: %w\n%s", err, output)
	}

	// Initialize mise in worktree if needed
	if err := mise.Initialize(worktreePath); err != nil {
		fmt.Printf("Warning: mise initialization failed: %v\n", err)
	}

	// Create work record in database
	if err := proj.DB.CreateWork(ctx, workID, worktreePath, branchName, baseBranch); err != nil {
		exec.Command("git", "worktree", "remove", worktreePath).Run()
		os.RemoveAll(workDir)
		return 0, data, fmt.Errorf("failed to create work record: %w", err)
	}

	fmt.Printf("Created work: %s\n", workID)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("Branch: %s\n", branchName)

	// Update step data with new work ID
	data.WorkID = workID
	data.BranchName = branchName

	return StepCollectBeads, data, nil
}

// stepCollectBeads collects all beads for the workflow
func stepCollectBeads(proj *project.Project, data StepData) (int, StepData, error) {
	mainRepoPath := proj.MainRepoPath()

	fmt.Printf("Collecting beads for: %s\n", data.BeadID)

	beadsToProcess, err := collectBeadsForAutomatedWorkflow(data.BeadID, mainRepoPath)
	if err != nil {
		return 0, data, fmt.Errorf("failed to collect beads: %w", err)
	}

	if len(beadsToProcess) == 0 {
		return 0, data, fmt.Errorf("no beads to process for %s", data.BeadID)
	}

	fmt.Printf("Collected %d bead(s):\n", len(beadsToProcess))
	var beadIDs []string
	for _, b := range beadsToProcess {
		fmt.Printf("  - %s: %s\n", b.ID, b.Title)
		beadIDs = append(beadIDs, b.ID)
	}

	data.BeadIDs = beadIDs
	return StepPlanTasks, data, nil
}

// stepPlanTasks plans tasks with auto-grouping
func stepPlanTasks(proj *project.Project, data StepData) (int, StepData, error) {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()

	fmt.Println("Planning tasks with auto-grouping...")

	// Get work
	work, err := proj.DB.GetWork(ctx, data.WorkID)
	if err != nil {
		return 0, data, fmt.Errorf("failed to get work: %w", err)
	}
	if work == nil {
		return 0, data, fmt.Errorf("work %s not found", data.WorkID)
	}

	// Convert bead IDs to Bead structs
	var beadList []beads.Bead
	for _, beadID := range data.BeadIDs {
		bead, err := beads.GetBeadInDir(beadID, mainRepoPath)
		if err != nil {
			fmt.Printf("Warning: failed to get bead %s: %v\n", beadID, err)
			continue
		}
		beadList = append(beadList, *bead)
	}

	if len(beadList) == 0 {
		return 0, data, fmt.Errorf("no valid beads to plan")
	}

	// Use auto-grouping to plan tasks
	if err := planAutoGroupForWork(proj, beadList, data.WorkID, work); err != nil {
		return 0, data, fmt.Errorf("failed to plan tasks: %w", err)
	}

	return StepExecuteTasks, data, nil
}

// stepExecuteTasks starts task execution
func stepExecuteTasks(proj *project.Project, data StepData) (int, StepData, error) {
	fmt.Println("Starting task execution...")

	// processWork spawns the first pending task
	if err := processWork(proj, data.WorkID); err != nil {
		return 0, data, fmt.Errorf("failed to execute tasks: %w", err)
	}

	return StepWaitCompletion, data, nil
}

// stepWaitCompletion polls for task completion
func stepWaitCompletion(proj *project.Project, data StepData) (int, StepData, error) {
	ctx := GetContext()

	fmt.Println("Waiting for task completion...")

	// Check work status
	tasks, err := proj.DB.GetWorkTasks(ctx, data.WorkID)
	if err != nil {
		return 0, data, fmt.Errorf("failed to get work tasks: %w", err)
	}

	pendingCount := 0
	processingCount := 0
	completedCount := 0
	failedCount := 0

	for _, task := range tasks {
		switch task.Status {
		case db.StatusPending:
			pendingCount++
		case db.StatusProcessing:
			processingCount++
		case db.StatusCompleted:
			completedCount++
		case db.StatusFailed:
			failedCount++
		}
	}

	fmt.Printf("Tasks: %d pending, %d processing, %d completed, %d failed\n",
		pendingCount, processingCount, completedCount, failedCount)

	// Check for failures
	if failedCount > 0 {
		return 0, data, fmt.Errorf("task(s) failed, aborting workflow")
	}

	// If tasks are still processing, stay in wait state
	if processingCount > 0 {
		fmt.Println("Tasks still processing, will check again...")
		return StepWaitCompletion, data, nil
	}

	// If there are pending tasks, spawn the next one
	if pendingCount > 0 {
		fmt.Println("Spawning next pending task...")
		if err := processWork(proj, data.WorkID); err != nil {
			return 0, data, fmt.Errorf("failed to spawn next task: %w", err)
		}
		return StepWaitCompletion, data, nil
	}

	// All tasks completed
	fmt.Println("All tasks completed!")
	return StepReviewFix, data, nil
}

// stepReviewFix runs the review-fix loop
func stepReviewFix(proj *project.Project, data StepData) (int, StepData, error) {
	ctx := GetContext()
	mainRepoPath := proj.MainRepoPath()
	const maxIterations = 5

	// Initialize review count if not set
	if data.ReviewCount >= maxIterations {
		return 0, data, fmt.Errorf("review-fix loop exceeded maximum iterations (%d)", maxIterations)
	}

	data.ReviewCount++
	fmt.Printf("Review iteration %d/%d\n", data.ReviewCount, maxIterations)

	// Capture pre-review beads for fallback detection
	preReviewBeadIDs := make(map[string]bool)
	preReviewBeads, err := beads.GetReadyBeadsInDir(mainRepoPath)
	if err != nil {
		return 0, data, fmt.Errorf("failed to get pre-review beads: %w", err)
	}
	for _, b := range preReviewBeads {
		preReviewBeadIDs[b.ID] = true
	}

	// Create and run review task
	reviewTaskID, err := createReviewTask(proj, data.WorkID)
	if err != nil {
		return 0, data, fmt.Errorf("failed to create review task: %w", err)
	}

	// Process the review task (blocking)
	if err := processTask(proj, reviewTaskID); err != nil {
		return 0, data, fmt.Errorf("review task failed: %w", err)
	}

	// Wait for review task to complete
	for {
		task, err := proj.DB.GetTask(ctx, reviewTaskID)
		if err != nil {
			return 0, data, fmt.Errorf("failed to get review task: %w", err)
		}
		if task.Status == db.StatusCompleted {
			break
		}
		if task.Status == db.StatusFailed {
			return 0, data, fmt.Errorf("review task failed: %s", task.ErrorMessage)
		}
		time.Sleep(5 * time.Second)
	}

	// Check if review created any issue beads
	hasIssues, err := checkForReviewIssues(proj, data.WorkID, preReviewBeadIDs)
	if err != nil {
		return 0, data, fmt.Errorf("failed to check for review issues: %w", err)
	}

	if !hasIssues {
		fmt.Println("Review passed - no issues found!")
		return StepCreatePR, data, nil
	}

	fmt.Println("Review found issues - fixing...")

	// Plan and execute fix tasks
	if err := planAndExecuteFixTasks(proj, data.WorkID); err != nil {
		return 0, data, fmt.Errorf("failed to fix review issues: %w", err)
	}

	// Go back to wait for completion
	return StepWaitCompletion, data, nil
}

// stepCreatePR creates the pull request
func stepCreatePR(proj *project.Project, data StepData) (int, StepData, error) {
	fmt.Println("Creating pull request...")

	if err := createWorkPR(proj, data.WorkID); err != nil {
		return 0, data, fmt.Errorf("failed to create PR: %w", err)
	}

	return StepCompleted, data, nil
}

// spawnNextStep spawns the next orchestration step via zellij
func spawnNextStep(proj *project.Project, workflowID string, step int) error {
	// Build the command to run
	coPath, err := os.Executable()
	if err != nil {
		coPath = "co" // Fallback to PATH lookup
	}

	command := fmt.Sprintf("%s orchestrate --workflow %s --step %d", coPath, workflowID, step)

	// Use zellij to run the command in the same pane
	cmd := exec.Command("zellij", "action", "write-chars", command+"\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to spawn next step: %w\n%s", err, output)
	}

	fmt.Printf("Spawned step %d\n", step)
	return nil
}

// InitWorkflow initializes a new workflow for the given bead
func InitWorkflow(proj *project.Project, beadID, baseBranch string) (string, error) {
	ctx := GetContext()

	// Generate workflow ID (the work ID will be created during StepCreateWork)
	workflowID := fmt.Sprintf("workflow-%d", time.Now().UnixNano())

	// Create initial step data
	stepData := StepData{
		BeadID:     beadID,
		BaseBranch: baseBranch,
	}
	stepDataJSON, err := json.Marshal(stepData)
	if err != nil {
		return "", fmt.Errorf("failed to serialize step data: %w", err)
	}

	// Create workflow state (work_id is empty until StepCreateWork completes)
	if err := proj.DB.CreateWorkflowState(ctx, workflowID, "", StepCreateWork, "pending", string(stepDataJSON)); err != nil {
		return "", fmt.Errorf("failed to create workflow state: %w", err)
	}

	return workflowID, nil
}
