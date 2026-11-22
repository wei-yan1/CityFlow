// Package xxljob wraps the XXL-JOB Go executor with an explicit lifecycle and
// error semantics. The SDK owns the executor protocol (register/heartbeat/run/
// kill/log), while this package owns the HTTP server so a go-zero service can
// start and stop it with the rest of its process.
package xxljob

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	xxl "github.com/xxl-job/xxl-job-executor-go"
)

const defaultTimeout = defaultTimeoutSeconds * time.Second

// Logger is the SDK logger contract. Keeping the alias here makes the wrapper
// usable without importing the SDK at call sites.
type Logger = xxl.Logger

type stdLogger struct{}

func (stdLogger) Info(format string, args ...interface{}) {
	log.Printf("[xxl-job] "+format, args...)
}

func (stdLogger) Error(format string, args ...interface{}) {
	log.Printf("[xxl-job] ERROR "+format, args...)
}

// Executor is a running or runnable XXL-JOB executor. A nil *Executor is valid
// and means scheduling is disabled.
type Executor struct {
	sdk       xxl.Executor
	port      string
	server    *http.Server
	listener  net.Listener
	startErr  error
	startOnce sync.Once
	stopOnce  sync.Once
}

// New builds and initializes the SDK executor. Initialization starts the SDK's
// registry loop; the HTTP listener is opened by Start so the caller controls
// when the process becomes reachable.
func New(cfg Settings, logger Logger) (*Executor, error) {
	cfg = cfg.WithEnv()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = stdLogger{}
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	options := []xxl.Option{
		xxl.ServerAddr(strings.TrimRight(strings.TrimSpace(cfg.ServerAddr), "/")),
		xxl.AccessToken(strings.TrimSpace(cfg.AccessToken)),
		xxl.ExecutorPort(strings.TrimSpace(cfg.ExecutorPort)),
		xxl.RegistryKey(strings.TrimSpace(cfg.RegistryKey)),
		xxl.SetLogger(logger),
		// Options.Timeout is public but the SDK exposes no Timeout option.
		// Setting it here keeps registry/callback calls from hanging shutdown.
		func(options *xxl.Options) { options.Timeout = timeout },
	}
	if ip := strings.TrimSpace(cfg.ExecutorIP); ip != "" {
		options = append(options, xxl.ExecutorIp(ip))
	}
	sdk := xxl.NewExecutor(options...)
	sdk.Init()
	return &Executor{sdk: sdk, port: strings.TrimSpace(cfg.ExecutorPort)}, nil
}

// Register binds a business handler to the JobHandler name configured in the
// XXL-JOB admin console. It must be called before Start.
func (e *Executor) Register(name string, fn xxl.TaskFunc) error {
	if e == nil {
		return errors.New("XXL-JOB executor is disabled")
	}
	if e.sdk == nil {
		return errors.New("XXL-JOB executor is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("XXL-JOB handler name is required")
	}
	if fn == nil {
		return errors.New("XXL-JOB handler function is required")
	}
	e.sdk.RegTask(name, fn)
	return nil
}

// Start opens the executor HTTP endpoints and returns immediately. Cancelling
// ctx stops the executor with the same path as Close.
func (e *Executor) Start(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.startOnce.Do(func() {
		port := e.port
		listener, err := net.Listen("tcp", ":"+port)
		if err != nil {
			e.startErr = fmt.Errorf("listen on XXL-JOB executor port %s: %w", port, err)
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/run", e.sdk.RunTask)
		mux.HandleFunc("/kill", e.sdk.KillTask)
		mux.HandleFunc("/log", e.sdk.TaskLog)
		mux.HandleFunc("/beat", e.sdk.Beat)
		mux.HandleFunc("/idleBeat", e.sdk.IdleBeat)
		e.listener = listener
		e.server = &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      3 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		go func() {
			if err := e.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("[xxl-job] executor server stopped: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			e.Stop()
		}()
	})
	return e.startErr
}

// Stop deregisters from the admin server and closes the HTTP listener. The
// SDK's registry loop is process-scoped and exits with the process; the
// deregistration call prevents the node from being selected during shutdown.
func (e *Executor) Stop() {
	if e == nil {
		return
	}
	e.stopOnce.Do(func() {
		if e.sdk != nil {
			e.sdk.Stop()
		}
		if e.server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := e.server.Shutdown(shutdownCtx); err != nil {
				_ = e.server.Close()
			}
		} else if e.listener != nil {
			_ = e.listener.Close()
		}
	})
}

// Handler adapts a business function that returns an error to the SDK's
// string-only TaskFunc. The SDK marks a task successful whenever the handler
// returns, so business failures must panic; the SDK recovers the panic and
// reports FailureCode to the admin server.
func Handler(fn func(context.Context, *xxl.RunReq) (string, error)) xxl.TaskFunc {
	return func(ctx context.Context, req *xxl.RunReq) (message string) {
		if fn == nil {
			panic("XXL-JOB handler is nil")
		}
		message, err := fn(ctx, req)
		if err != nil {
			panic(err)
		}
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				panic(err)
			}
		}
		return message
	}
}
