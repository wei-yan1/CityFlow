package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"cityflow/app/notification/cmd/rpc/internal/config"
	"cityflow/app/notification/cmd/rpc/internal/server"
	"cityflow/app/notification/cmd/rpc/internal/svc"
	"cityflow/app/notification/cmd/rpc/pb"
	"cityflow/internal/platform/authn"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/notification/cmd/rpc/etc/notification.yaml", "the config file")

func main() {
	flag.Parse()

	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("notification bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("notification close: %v", err)
		}
	}()
	eventCtx, stopEvents := context.WithCancel(context.Background())
	defer stopEvents()
	ctx.StartEvents(eventCtx)

	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	if err := ctx.StartJobs(jobCtx); err != nil {
		log.Fatalf("notification jobs: %v", err)
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterNotificationServer(grpcServer, server.NewNotificationServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, func(_ context.Context, subject, _ string) (bool, error) {
		return subject != "", nil
	}, "/notification.Notification/Health"))
	defer s.Stop()

	fmt.Printf("Starting notification rpc server at %s...\n", c.ListenOn)
	s.Start()
}
