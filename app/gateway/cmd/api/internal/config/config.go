package config

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	rest.RestConf
	IdentityRPC                zrpc.RpcClientConf
	TaskRPC                    zrpc.RpcClientConf
	DiscoveryRPC               zrpc.RpcClientConf
	PaymentRPC                 zrpc.RpcClientConf
	TrackingRPC                zrpc.RpcClientConf
	PromotionRPC               zrpc.RpcClientConf
	NotificationRPC            zrpc.RpcClientConf
	ReactionRPC                zrpc.RpcClientConf
	UploadDir                  string `json:",optional"`
	TrackingStreamURL          string `json:",default=http://127.0.0.1:8093"`
	JWTSecret                  string `json:",optional"`
	AuthStatusCacheTTLSeconds  int    `json:",default=5"`
	RateLimitRPS               int    `json:",default=100"`
	RateLimitBurst             int    `json:",default=200"`
	RequestTimeoutSeconds      int    `json:",default=15"`
	StartupReadyTimeoutSeconds int    `json:",default=60"`
}
