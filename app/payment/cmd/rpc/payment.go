package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"cityflow/app/payment/cmd/rpc/internal/config"
	"cityflow/app/payment/cmd/rpc/internal/server"
	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/payment/cmd/rpc/etc/payment.yaml", "the config file")

func main() {
	flag.Parse()
	_ = godotenv.Load(".env")

	var c config.Config
	conf.MustLoad(*configFile, &c)
	if listen := strings.TrimSpace(os.Getenv("PAYMENT_LISTEN_ON")); listen != "" {
		c.ListenOn = listen
	}
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("payment bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("payment close: %v", err)
		}
	}()
	ctx.Payments.StartNotifyBuffer()
	defer ctx.Payments.StopNotifyBuffer()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterPaymentServer(grpcServer, server.NewPaymentServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, ctx.AccountActive,
		"/payment.Payment/Health",
		"/payment.Payment/Notify",
	))
	defer s.Stop()
	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerCtx = rpcctx.WithBearer(workerCtx, authn.PaymentWorkerToken(ctx.Secret))
	settlementWorkerEnabled := envEnabled("PAYMENT_SETTLEMENT_WORKER_ENABLED", true)
	reconciliationWorkerEnabled := envEnabled("PAYMENT_RECONCILIATION_WORKER_ENABLED", true)
	inboxRecoveryWorkerEnabled := envEnabled("PAYMENT_INBOX_RECOVERY_WORKER_ENABLED", true)
	var workerWG sync.WaitGroup
	if settlementWorkerEnabled {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			log.Printf("payment settlement worker started interval=%s", ctx.WorkerInterval)
			ctx.Payments.RunSettlementWorker(workerCtx, ctx.WorkerInterval)
		}()
	}
	if reconciliationWorkerEnabled {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			log.Printf("payment reconciliation worker started interval=%s", ctx.WorkerInterval)
			ctx.Payments.RunReconciliationWorker(workerCtx, ctx.WorkerInterval)
		}()
	}
	if inboxRecoveryWorkerEnabled {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			log.Printf("payment inbox recovery worker started interval=30s")
			ctx.Payments.RunInboxRecoveryWorker(workerCtx, 30*time.Second)
		}()
	}
	if ctx.Store != nil {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			logDBPoolStats(workerCtx, ctx.Store.DBStats)
		}()
	}
	defer func() {
		stopWorker()
		workerWG.Wait()
	}()

	eventCtx, stopEvents := context.WithCancel(context.Background())
	ctx.StartEvents(eventCtx)
	defer stopEvents()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}

func envEnabled(name string, defaultValue bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("warning: invalid %s=%q; using default %t", name, raw, defaultValue)
		return defaultValue
	}
}

// logDBPoolStats publishes database/sql pool counters every five seconds so a
// load run can prove whether handlers waited for a connection. WaitCount is
// cumulative; compare it before and after the run rather than reading one line.
func logDBPoolStats(ctx context.Context, stats func() sql.DBStats) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s := stats()
			log.Printf("payment db pool open=%d in_use=%d idle=%d max_open=%d wait_count=%d wait=%s",
				s.OpenConnections, s.InUse, s.Idle, s.MaxOpenConnections, s.WaitCount, s.WaitDuration)
		}
	}
}
