package mise_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/newhook/co/internal/mise"
	"github.com/stretchr/testify/require"
)

func TestMockImplementsInterface(t *testing.T) {
	// Compile-time check that MiseOperationsMock implements Operations
	var _ mise.Operations = (*mise.MiseOperationsMock)(nil)
}

// TestMiseOperationsMock verifies the mock works correctly with function fields.
func TestMiseOperationsMock(t *testing.T) {
	t.Run("IsManaged returns configured value", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{
			IsManagedFunc: func() bool {
				return true
			},
		}

		require.True(t, mock.IsManaged(), "expected IsManaged to return true")

		// Verify calls are tracked
		calls := mock.IsManagedCalls()
		require.Len(t, calls, 1)
	})

	t.Run("HasTask returns configured value based on task name", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{
			HasTaskFunc: func(taskName string) bool {
				return taskName == "setup" || taskName == "build"
			},
		}

		require.True(t, mock.HasTask("setup"), "expected HasTask('setup') to return true")
		require.True(t, mock.HasTask("build"), "expected HasTask('build') to return true")
		require.False(t, mock.HasTask("nonexistent"), "expected HasTask('nonexistent') to return false")

		// Verify calls are tracked with correct arguments
		calls := mock.HasTaskCalls()
		require.Len(t, calls, 3)
		require.Equal(t, "setup", calls[0].TaskName)
	})

	t.Run("Trust and Install track calls", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{
			TrustFunc: func() error {
				return nil
			},
			InstallFunc: func() error {
				return nil
			},
		}

		require.NoError(t, mock.Trust())
		require.NoError(t, mock.Install())

		require.Len(t, mock.TrustCalls(), 1, "expected 1 Trust call")
		require.Len(t, mock.InstallCalls(), 1, "expected 1 Install call")
	})

	t.Run("RunTask tracks task name", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{
			RunTaskFunc: func(taskName string) error {
				if taskName == "failing-task" {
					return errors.New("task failed")
				}
				return nil
			},
		}

		require.NoError(t, mock.RunTask("setup"))
		require.Error(t, mock.RunTask("failing-task"), "expected error for failing task")

		calls := mock.RunTaskCalls()
		require.Len(t, calls, 2)
	})

	t.Run("Exec tracks command and args", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{
			ExecFunc: func(command string, args ...string) ([]byte, error) {
				return []byte("output"), nil
			},
		}

		output, err := mock.Exec("npm", "install", "--save-dev")
		require.NoError(t, err)
		require.Equal(t, "output", string(output))

		calls := mock.ExecCalls()
		require.Len(t, calls, 1)
		require.Equal(t, "npm", calls[0].Command)
		require.Equal(t, []string{"install", "--save-dev"}, calls[0].Args)
	})

	t.Run("Initialize tracks calls", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{
			InitializeFunc: func() error {
				return nil
			},
		}

		require.NoError(t, mock.Initialize())

		require.Len(t, mock.InitializeCalls(), 1, "expected 1 Initialize call")
	})

	t.Run("InitializeWithOutput tracks writer", func(t *testing.T) {
		var buf bytes.Buffer
		mock := &mise.MiseOperationsMock{
			InitializeWithOutputFunc: func(w io.Writer) error {
				return nil
			},
		}

		require.NoError(t, mock.InitializeWithOutput(&buf))

		calls := mock.InitializeWithOutputCalls()
		require.Len(t, calls, 1, "expected 1 InitializeWithOutput call")
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &mise.MiseOperationsMock{}

		// Without setting function, mock returns zero values
		require.False(t, mock.IsManaged(), "expected false when IsManagedFunc is nil")
		require.False(t, mock.HasTask("any"), "expected false when HasTaskFunc is nil")
		require.NoError(t, mock.Trust(), "expected nil error when TrustFunc is nil")

		output, err := mock.Exec("cmd")
		require.NoError(t, err)
		require.Nil(t, output, "expected nil output when ExecFunc is nil")
	})
}
