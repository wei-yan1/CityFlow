package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	DataSource                string             `json:",optional"`
	JWTSecret                 string             `json:",optional"`
	RabbitMQURL               string             `json:",optional"`
	TrustedPlaceTTL           int                `json:",default=1800"`
	AuthStatusCacheTTLSeconds int                `json:",default=5"`
	IdentityRPC               zrpc.RpcClientConf `json:",optional"`
	DiscoveryRPC              zrpc.RpcClientConf `json:",optional"`
	TrackingRPC               zrpc.RpcClientConf `json:",optional"`
	PaymentRPC                zrpc.RpcClientConf `json:",optional"`
	ReactionRPC               zrpc.RpcClientConf `json:",optional"`
}
