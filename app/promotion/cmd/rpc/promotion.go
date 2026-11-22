package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"cityflow/app/promotion/cmd/rpc/internal/config"
	"cityflow/app/promotion/cmd/rpc/internal/server"
	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"
	"cityflow/internal/platform/authn"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/promotion/cmd/rpc/etc/promotion.yaml", "the config file")

func main() {
	flag.Parse()

	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("promotion bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("promotion close: %v", err)
		}
	}()

	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	if err := ctx.StartJobs(jobCtx); err != nil {
		log.Fatalf("promotion jobs: %v", err)
	}
	ctx.StartWorkers(jobCtx)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterPromotionServer(grpcServer, server.NewPromotionServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, func(_ context.Context, subject, _ string) (bool, error) {
		return subject != "", nil
	}, "/promotion.Promotion/Health"))
	defer s.Stop()

	fmt.Printf("Starting promotion rpc server at %s...\n", c.ListenOn)
	s.Start()
}
