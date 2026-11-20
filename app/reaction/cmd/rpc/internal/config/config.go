package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	zrpc.RpcServerConf
	DataSource  string             `json:",optional"`
	JWTSecret   string             `json:",optional"`
	IdentityRPC zrpc.RpcClientConf `json:",optional"`

	ReactionRedis struct {
		Host     string `json:",default=127.0.0.1:6380"`
		Password string `json:",optional"`
		DB       int    `json:",default=0"`
	} `json:"ReactionRedis,optional"`

	MySQL struct {
		DSN string `json:",optional"`
	} `json:",optional"`

	RabbitMQ struct {
		URL      string `json:",optional"`
		Exchange string `json:",default=cf.events"`
	} `json:",optional"`

	ProjectionTypes []string `json:",optional"`
	WorkersEnabled  bool     `json:",default=true"`
	TickerSeconds   int      `json:",default=5"`
	TickerBatchSize int      `json:",default=1000"`
	OpsBatchSize    int      `json:",default=1000"`
	OpsBlockMillis  int      `json:",default=200"`
	MaxSummaryIDs   int      `json:",default=50"`
}

func (c Config) ReactionDSN() string { return c.MySQL.DSN }
