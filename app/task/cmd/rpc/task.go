package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"cityflow/app/task/cmd/rpc/internal/config"
	"cityflow/app/task/cmd/rpc/internal/server"
	"cityflow/app/task/cmd/rpc/internal/svc"
	"cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpckit"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/task/cmd/rpc/etc/task.yaml", "the config file")

func main() {
	flag.Parse()

	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	if raw := strings.TrimSpace(os.Getenv("TASK_PAYMENT_RPC_ENDPOINTS")); raw != "" {
		if endpoints := rpckit.EndpointList(raw); len(endpoints) > 0 {
			c.PaymentRPC.Endpoints = endpoints
		}
	}
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("task bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("task close: %v", err)
		}
	}()

	eventCtx, stopEvents := context.WithCancel(context.Background())
	ctx.StartEvents(eventCtx)
	if err := ctx.StartJobs(eventCtx); err != nil {
		stopEvents()
		log.Fatalf("task job executor: %v", err)
	}
	defer stopEvents()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		task.RegisterTaskServer(grpcServer, server.NewTaskServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	userAuth := authn.UnaryServerInterceptor(ctx.Secret, ctx.AccountActive,
		"/task.Task/Health",
	)
	s.AddUnaryInterceptors(authn.PaymentWorkerAuth(ctx.Secret, task.Task_ListTasksInternal_FullMethodName, userAuth))
	defer s.Stop()

	fmt.Printf("Starting task rpc server at %s...\n", c.ListenOn)
	s.Start()
}
