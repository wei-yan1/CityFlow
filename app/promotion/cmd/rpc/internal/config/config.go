package config

import (
	"cityflow/internal/platform/xxljob"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	DataSource  string          `json:",optional"`
	JWTSecret   string          `json:",optional"`
	RabbitMQURL string          `json:",optional"`
	XXLJob      xxljob.Settings `json:",optional"`

	RedisAddr     string `json:",optional"`
	RedisPassword string `json:",optional"`
	RedisDB       int    `json:",default=0"`

	// ClaimMode=sync 走同步 DB 事务；reserve 走 Redis 预扣 + Stream 异步落库。
	ClaimMode               string `json:",default=sync"`
	ClaimWaitMS             int    `json:",default=300"`
	ClaimBatch              int64  `json:",default=50"`
	ClaimWorkers            int    `json:",default=2"`
	ClaimStream             string `json:",default=prs:claim:stream"`
	ClaimGroup              string `json:",default=promotion-claim"`
	ClaimResultTTLSeconds   int    `json:",default=86400"`
	ClaimReclaimIdleSeconds int    `json:",default=30"`
}
