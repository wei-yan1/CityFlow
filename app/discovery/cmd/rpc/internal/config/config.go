package config

import (
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DataSource       string             `json:",optional"`
	JWTSecret        string             `json:",optional"`
	InternalJobToken string             `json:",optional"`
	UploadDir        string             `json:",optional"`
	MapConfig        string             `json:",optional"`
	TrustedTTL       int                `json:",default=1800"`
	IdentityRPC      zrpc.RpcClientConf `json:",optional"`
	TaskRPC          zrpc.RpcClientConf `json:",optional"`
	ReactionRPC      zrpc.RpcClientConf `json:",optional"`
	RabbitMQURL      string             `json:",optional"`
	XXLJob           xxljob.Settings    `json:",optional"`
}
