package config

import (
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DataSource          string          `json:",optional"`
	JWTSecret           string          `json:",optional"`
	RabbitMQURL         string          `json:",optional"`
	ReadRetentionDays   int             `json:",default=90"`
	UnreadRetentionDays int             `json:",default=180"`
	XXLJob              xxljob.Settings `json:",optional"`
}
