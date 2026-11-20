package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	JWTSecret     string             `json:",optional"`
	RedisAddr     string             `json:",optional"`
	RedisPassword string             `json:",optional"`
	RedisDB       int                `json:",default=0"`
	TaskRPC       zrpc.RpcClientConf `json:",optional"`
	IdentityRPC   zrpc.RpcClientConf `json:",optional"`
	StreamListen  string             `json:",default=127.0.0.1:8093"`
}
