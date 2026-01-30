// Package testutil provides shared test utilities and moq-generated mocks.
//
// Mocks are generated using moq (github.com/matryer/moq) and can be regenerated with:
//
//	mise run generate
//
// or
//
//	go generate ./...
//
// Each mock is generated from the interface definition in its source package.
// The mocks use function-field style, allowing tests to customize behavior per-test:
//
//	mock := &testutil.GitOperationsMock{
//	    PushSetUpstreamFunc: func(ctx context.Context, branch, dir string) error {
//	        return nil
//	    },
//	}
package testutil
