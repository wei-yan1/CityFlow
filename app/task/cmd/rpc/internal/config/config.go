package config

import (
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DataSource                string             `json:",optional"`
	JWTSecret                 string             `json:",optional"`
	InternalJobToken          string             `json:",optional"`
	RabbitMQURL               string             `json:",optional"`
	AuthStatusCacheTTLSeconds int                `json:",default=5"`
	XXLJob                    xxljob.Settings    `json:",optional"`
	XXLJobRankLimit           int                `json:",default=10"`
	IdentityRPC               zrpc.RpcClientConf `json:",optional"`
	DiscoveryRPC              zrpc.RpcClientConf `json:",optional"`
	TrackingRPC               zrpc.RpcClientConf `json:",optional"`
	PaymentRPC                zrpc.RpcClientConf `json:",optional"`
	ReactionRPC               zrpc.RpcClientConf `json:",optional"`
}
