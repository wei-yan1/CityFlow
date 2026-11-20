package svc

import (
	"strings"
	"testing"

	"cityflow/app/gateway/cmd/api/internal/config"

	"github.com/zeromicro/go-zero/zrpc"
)

func rpc(endpoint string) zrpc.RpcClientConf {
	return zrpc.RpcClientConf{Endpoints: []string{endpoint}, NonBlock: true}
}

func TestNewServiceContextRequiresEveryDomainEndpoint(t *testing.T) {
	base := config.Config{
		IdentityRPC: rpc("127.0.0.1:8090"),
		TaskRPC:     rpc("127.0.0.1:8091"),
		PaymentRPC:  rpc("127.0.0.1:8094"),
		TrackingRPC: rpc("127.0.0.1:8095"),
	}
	if _, err := NewServiceContext(base); err == nil || !strings.Contains(err.Error(), "DiscoveryRPC") {
		t.Fatalf("expected missing discovery error, got %v", err)
	}
}
