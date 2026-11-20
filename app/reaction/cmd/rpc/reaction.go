package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"cityflow/app/reaction/cmd/rpc/internal/config"
	"cityflow/app/reaction/cmd/rpc/internal/server"
	"cityflow/app/reaction/cmd/rpc/internal/svc"
	"cityflow/app/reaction/cmd/rpc/pb"
	"cityflow/internal/platform/authn"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/reaction/cmd/rpc/etc/reaction.yaml", "the config file")

func main() {
	flag.Parse()
	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("reaction bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("reaction close: %v", err)
		}
	}()

	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	ctx.StartWorkers(workCtx)

	grpcServer := zrpc.MustNewServer(c.RpcServerConf, func(server *grpc.Server) {
		pb.RegisterReactionServer(server, serverlogic(ctx))
		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(server)
		}
	})
	grpcServer.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, ctx.AccountActive,
		"/reaction.Reaction/Health",
		"/reaction.Reaction/GetReactionState",
	))
	defer grpcServer.Stop()

	fmt.Printf("Starting reaction rpc server at %s...\n", c.ListenOn)
	grpcServer.Start()
}

func serverlogic(ctx *svc.ServiceContext) pb.ReactionServer {
	return server.NewReactionServer(ctx)
}
