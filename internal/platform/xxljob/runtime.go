package xxljob

import (
	"context"
	"errors"
	"sync"

	xxl "github.com/xxl-job/xxl-job-executor-go"
)

// RunReq is the SDK request exposed under this package so service code does
// not import the executor SDK directly.
type RunReq = xxl.RunReq

// Func is a business job handler. Errors are converted to the SDK's failure
// protocol by Runtime; callers only return normal Go errors.
type Func func(context.Context, *RunReq) (string, error)

// Registration pairs the JobHandler name configured in the XXL-JOB admin
// console with the service-owned implementation.
type Registration struct {
	Name    string
	Handler Func
}

// Runtime is the common executor bean held by every participating service's
// ServiceContext. A nil Runtime is valid and means scheduling is disabled.
type Runtime struct {
	executor  *Executor
	startOnce sync.Once
	startErr  error
}

// NewRuntime is the single bootstrap entry shared by all services. It applies
// environment overrides and returns (nil, nil) when scheduling is disabled, so
// callers do not need a per-service Enabled branch.
func NewRuntime(settings Settings, logger Logger) (*Runtime, error) {
	executor, err := New(settings, logger)
	if err != nil {
		return nil, err
	}
	if executor == nil {
		return nil, nil
	}
	return &Runtime{executor: executor}, nil
}

// Register binds one handler. It is safe on a nil Runtime and must be called
// before Start; registering against a disabled runtime is a no-op.
func (r *Runtime) Register(name string, fn Func) error {
	if r == nil {
		return nil
	}
	if fn == nil {
		return errors.New("XXL-JOB handler function is required")
	}
	return r.executor.Register(name, Handler(fn))
}

// Start registers the supplied handlers and opens the executor endpoints once.
// Later calls are no-ops. Cancelling ctx stops the executor through the same
// path as Stop.
func (r *Runtime) Start(ctx context.Context, registrations ...Registration) error {
	if r == nil {
		return nil
	}
	r.startOnce.Do(func() {
		for _, registration := range registrations {
			if err := r.Register(registration.Name, registration.Handler); err != nil {
				r.startErr = err
				return
			}
		}
		r.startErr = r.executor.Start(ctx)
	})
	return r.startErr
}

// Stop deregisters the executor and closes its HTTP listener. It is safe on a
// nil Runtime and may be called after a failed Start.
func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	r.executor.Stop()
}
