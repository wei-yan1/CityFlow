package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"cityflow/app/identity/cmd/rpc/internal/config"
	"cityflow/app/identity/cmd/rpc/internal/server"
	"cityflow/app/identity/cmd/rpc/internal/svc"
	"cityflow/app/identity/cmd/rpc/pb"
	"cityflow/internal/platform/authn"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "app/identity/cmd/rpc/etc/identity.yaml", "the config file")

func main() {
	flag.Parse()

	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("identity bootstrap: %v", err)
	}
	defer func() {
		if err := ctx.Close(); err != nil {
			log.Printf("identity close: %v", err)
		}
	}()

	if ctx.Relay != nil {
		relayCtx, stopRelay := context.WithCancel(context.Background())
		go ctx.Relay.Run(relayCtx)
		defer stopRelay()
	}

	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	if err := ctx.StartJobs(jobCtx); err != nil {
		log.Fatalf("identity jobs: %v", err)
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		identity.RegisterIdentityServer(grpcServer, server.NewIdentityServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, ctx.AccountActive,
		"/identity.Identity/Login",
		"/identity.Identity/Refresh",
		"/identity.Identity/Logout",
		"/identity.Identity/Health",
		"/identity.Identity/Register",
		"/identity.Identity/RequestPasswordReset",
		"/identity.Identity/ResetPassword",
		"/identity.Identity/SubmitRiderRegistration",
	))
	defer s.Stop()

	fmt.Printf("Starting identity rpc server at %s...\n", c.ListenOn)
	s.Start()
}
