package config

import (
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DataSource    string             `json:",optional"`
	JWTSecret     string             `json:",optional"`
	RabbitMQURL   string             `json:",optional"`
	SeedDemo      bool               `json:",default=false"`
	RedisAddr     string             `json:",optional"`
	RedisPassword string             `json:",optional"`
	RedisDB       int                `json:",default=0"`
	TaskRpc       zrpc.RpcClientConf `json:",optional"`
	XXLJob        xxljob.Settings    `json:",optional"`
}
