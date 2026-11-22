package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"cityflow/app/discovery/cmd/rpc/internal/config"
	"cityflow/app/discovery/cmd/rpc/internal/server"
	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/platform/authn"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/discovery/cmd/rpc/etc/discovery.yaml", "the config file")

func main() {
	flag.Parse()

	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("discovery bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("discovery close: %v", err)
		}
	}()

	eventCtx, stopEvents := context.WithCancel(context.Background())
	defer stopEvents()
	ctx.StartEvents(eventCtx)

	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	if err := ctx.StartJobs(jobCtx); err != nil {
		log.Fatalf("discovery jobs: %v", err)
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterDiscoveryServer(grpcServer, server.NewDiscoveryServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, ctx.AccountActive,
		"/discovery.Discovery/Health",
		"/discovery.Discovery/MapConfig",
		"/discovery.Discovery/MapMerchants",
		"/discovery.Discovery/MapSearch",
		"/discovery.Discovery/MapNearby",
		"/discovery.Discovery/MapRiding",
		"/discovery.Discovery/MapCity",
		"/discovery.Discovery/MapReverse",
		"/discovery.Discovery/MapDistrict",
		"/discovery.Discovery/PlaceDetail",
		"/discovery.Discovery/ListPlaceComments",
		"/discovery.Discovery/ListPlaceCommentThread",
		"/discovery.Discovery/ListHotMerchants",
	))
	defer s.Stop()

	fmt.Printf("Starting discovery rpc server at %s...\n", c.ListenOn)
	s.Start()
}
