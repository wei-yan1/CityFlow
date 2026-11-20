package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"cityflow/app/gateway/cmd/api/internal/config"
	"cityflow/app/gateway/cmd/api/internal/handler"
	"cityflow/app/gateway/cmd/api/internal/middleware"
	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/rpckit"

	"github.com/joho/godotenv"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "app/gateway/cmd/api/etc/gateway-api.yaml", "the config file")

func main() {
	flag.Parse()

	_ = godotenv.Load(".env")
	var c config.Config
	conf.MustLoad(*configFile, &c)
	if raw := strings.TrimSpace(os.Getenv("GATEWAY_PAYMENT_RPC_ENDPOINTS")); raw != "" {
		if endpoints := rpckit.EndpointList(raw); len(endpoints) > 0 {
			c.PaymentRPC.Endpoints = endpoints
		}
	}
	serviceContext, err := svc.NewServiceContext(c)
	if err != nil {
		log.Fatalf("gateway bootstrap: %v", err)
	}

	// The public edge must not bind until Identity, Task, Discovery, Payment and
	// Tracking are all answering health RPCs. This prevents a partially started
	// stack from serving traffic or silently falling back to old code.
	startupTimeout := time.Duration(c.StartupReadyTimeoutSeconds) * time.Second
	if startupTimeout <= 0 {
		startupTimeout = 60 * time.Second
	}
	log.Printf("waiting up to %s for domain services", startupTimeout)
	if err := serviceContext.WaitReady(context.Background(), startupTimeout); err != nil {
		log.Fatalf("gateway dependencies are not ready: %v", err)
	}

	requestID := middleware.RequestID()
	accessWriter := middleware.NewAccessLogWriter(2048, 128, 100*time.Millisecond, nil)
	accessWriter.Start()
	defer accessWriter.Close()
	accessLog := middleware.AccessLogWithWriter(accessWriter)
	recoverPanic := middleware.Recover()
	rateLimit := middleware.RateLimit(c.RateLimitRPS, c.RateLimitBurst)
	edge := []rest.Middleware{requestID, accessLog, recoverPanic, rateLimit}

	pages := middleware.Chain(handler.Pages(serviceContext), edge...)
	server := rest.MustNewServer(c.RestConf, rest.WithNotFoundHandler(pages))
	defer server.Stop()
	server.Use(requestID)
	server.Use(accessLog)
	server.Use(recoverPanic)
	server.Use(rateLimit)
	handler.RegisterHandlers(server, serviceContext)

	log.Printf("CityFlow Gateway listening on %s:%d; pages served from embedded web tree", c.Host, c.Port)
	server.Start()
}
