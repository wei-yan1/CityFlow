package config

import (
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DataSource            string             `json:",optional"`
	JWTSecret             string             `json:",optional"`
	RabbitMQURL           string             `json:",optional"`
	IdentityRPC           zrpc.RpcClientConf `json:",optional"`
	TaskRPC               zrpc.RpcClientConf `json:",optional"`
	PromotionRPC          zrpc.RpcClientConf `json:",optional"`
	Provider              string             `json:",optional"`
	MockBaseURL           string             `json:",optional"`
	FeeBPS                int64              `json:",default=0"`
	ExpiryMinutes         int                `json:",default=30"`
	SettleIntervalSeconds int                `json:",default=30"`
	AllowMockPay          bool               `json:",default=false"`
	MaxAttempts           int                `json:",default=8"`
	NotifyRetentionDays   int                `json:",default=365"`
	XXLJob                xxljob.Settings    `json:",optional"`
}
