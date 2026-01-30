package mise_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/newhook/co/internal/mise"
	"github.com/newhook/co/internal/testutil"
)

func TestMockImplementsInterface(t *testing.T) {
	// Compile-time check that MiseOperationsMock implements Operations
	var _ mise.Operations = (*testutil.MiseOperationsMock)(nil)
}

// TestMiseOperationsMock verifies the mock works correctly with function fields.
func TestMiseOperationsMock(t *testing.T) {
	t.Run("IsManaged returns configured value", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{
			IsManagedFunc: func() bool {
				return true
			},
		}

		if !mock.IsManaged() {
			t.Error("expected IsManaged to return true")
		}

		// Verify calls are tracked
		calls := mock.IsManagedCalls()
		if len(calls) != 1 {
			t.Errorf("expected 1 call, got %d", len(calls))
		}
	})

	t.Run("HasTask returns configured value based on task name", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{
			HasTaskFunc: func(taskName string) bool {
				return taskName == "setup" || taskName == "build"
			},
		}

		if !mock.HasTask("setup") {
			t.Error("expected HasTask('setup') to return true")
		}
		if !mock.HasTask("build") {
			t.Error("expected HasTask('build') to return true")
		}
		if mock.HasTask("nonexistent") {
			t.Error("expected HasTask('nonexistent') to return false")
		}

		// Verify calls are tracked with correct arguments
		calls := mock.HasTaskCalls()
		if len(calls) != 3 {
			t.Errorf("expected 3 calls, got %d", len(calls))
		}
		if calls[0].TaskName != "setup" {
			t.Errorf("expected first call with 'setup', got %q", calls[0].TaskName)
		}
	})

	t.Run("Trust and Install track calls", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{
			TrustFunc: func() error {
				return nil
			},
			InstallFunc: func() error {
				return nil
			},
		}

		if err := mock.Trust(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := mock.Install(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(mock.TrustCalls()) != 1 {
			t.Error("expected 1 Trust call")
		}
		if len(mock.InstallCalls()) != 1 {
			t.Error("expected 1 Install call")
		}
	})

	t.Run("RunTask tracks task name", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{
			RunTaskFunc: func(taskName string) error {
				if taskName == "failing-task" {
					return errors.New("task failed")
				}
				return nil
			},
		}

		if err := mock.RunTask("setup"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if err := mock.RunTask("failing-task"); err == nil {
			t.Error("expected error for failing task")
		}

		calls := mock.RunTaskCalls()
		if len(calls) != 2 {
			t.Errorf("expected 2 calls, got %d", len(calls))
		}
	})

	t.Run("Exec tracks command and args", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{
			ExecFunc: func(command string, args ...string) ([]byte, error) {
				return []byte("output"), nil
			},
		}

		output, err := mock.Exec("npm", "install", "--save-dev")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if string(output) != "output" {
			t.Errorf("expected 'output', got %q", string(output))
		}

		calls := mock.ExecCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Command != "npm" {
			t.Errorf("expected command 'npm', got %s", calls[0].Command)
		}
		if len(calls[0].Args) != 2 || calls[0].Args[0] != "install" || calls[0].Args[1] != "--save-dev" {
			t.Errorf("expected args ['install', '--save-dev'], got %v", calls[0].Args)
		}
	})

	t.Run("Initialize tracks calls", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{
			InitializeFunc: func() error {
				return nil
			},
		}

		if err := mock.Initialize(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(mock.InitializeCalls()) != 1 {
			t.Error("expected 1 Initialize call")
		}
	})

	t.Run("InitializeWithOutput tracks writer", func(t *testing.T) {
		var buf bytes.Buffer
		mock := &testutil.MiseOperationsMock{
			InitializeWithOutputFunc: func(w io.Writer) error {
				return nil
			},
		}

		if err := mock.InitializeWithOutput(&buf); err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		calls := mock.InitializeWithOutputCalls()
		if len(calls) != 1 {
			t.Error("expected 1 InitializeWithOutput call")
		}
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &testutil.MiseOperationsMock{}

		// Without setting function, mock returns zero values
		if mock.IsManaged() {
			t.Error("expected false when IsManagedFunc is nil")
		}
		if mock.HasTask("any") {
			t.Error("expected false when HasTaskFunc is nil")
		}
		if err := mock.Trust(); err != nil {
			t.Error("expected nil error when TrustFunc is nil")
		}
		output, err := mock.Exec("cmd")
		if err != nil || output != nil {
			t.Error("expected nil output and nil error when ExecFunc is nil")
		}
	})
}
