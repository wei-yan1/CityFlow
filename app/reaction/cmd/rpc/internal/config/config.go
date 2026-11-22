package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	JWTSecret   string             `json:",optional"`
	IdentityRPC zrpc.RpcClientConf `json:",optional"`

	RedisAddr     string `json:",optional"`
	RedisPassword string `json:",optional"`
	RedisDB       int    `json:",default=0"`

	RabbitMQURL     string   `json:",optional"`
	LikeExchange    string   `json:",default=like.record.topic"`
	TickerSeconds   int      `json:",default=20"`
	TickerBatchSize int64    `json:",default=30"`
	ProjectionTypes []string `json:",optional"`
	MaxSummaryIDs   int      `json:",default=50"`
}
