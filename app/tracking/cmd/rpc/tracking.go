package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"time"

	"cityflow/app/tracking/cmd/rpc/internal/config"
	"cityflow/app/tracking/cmd/rpc/internal/server"
	"cityflow/app/tracking/cmd/rpc/internal/stream"
	"cityflow/app/tracking/cmd/rpc/internal/svc"
	"cityflow/app/tracking/cmd/rpc/pb"
	"cityflow/internal/platform/authn"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/tracking.yaml", "the config file")

func main() {
	flag.Parse()
	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		logx.Must(err)
	}
	defer ctx.Close()

	// The WSS stream is an internal HTTP endpoint in the same process so it can
	// subscribe to the in-process position bus that Report publishes to. Bind it
	// before the gRPC server: if the stream port is unavailable, Tracking is not
	// fully started and the process must not advertise healthy.
	listener, err := net.Listen("tcp", c.StreamListen)
	if err != nil {
		logx.Must(fmt.Errorf("tracking stream listener %s: %w", c.StreamListen, err))
	}
	streamSrv := &http.Server{Handler: stream.Handler(ctx), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		fmt.Printf("Starting tracking stream at %s...\n", c.StreamListen)
		if err := streamSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			logx.Errorf("tracking stream: %v", err)
		}
	}()
	defer streamSrv.Close()

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterTrackingServer(grpcServer, server.NewTrackingServer(ctx))
		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	s.AddUnaryInterceptors(authn.UnaryServerInterceptor(ctx.Secret, func(callCtx context.Context, subject, token string) (bool, error) {
		return ctx.Status.IsActive(callCtx, subject, token)
	},
		"/tracking.Tracking/Health",
	))
	defer s.Stop()
	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
