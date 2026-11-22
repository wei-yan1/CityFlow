package svc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cityflow/app/notification/cmd/rpc/internal/config"
	"cityflow/app/notification/internal/adapter/mysql"
	"cityflow/app/notification/internal/application/notificationapp"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/mq"
	platformmysql "cityflow/internal/platform/mysql"
	"cityflow/internal/platform/xxljob"
)

type ServiceContext struct {
	Config          config.Config
	Secret          []byte
	MQURL           string
	DB              *platformmysql.DB
	Store           *notificationadapter.Store
	Service         *notificationapp.Service
	Consumer        *mq.Consumer
	XXLJob          *xxljob.Runtime
	ReadRetention   time.Duration
	UnreadRetention time.Duration
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	dsn := strings.TrimSpace(c.DataSource)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("NOTIFICATION_DATABASE_DSN"))
	}
	if dsn == "" {
		return nil, errors.New("NOTIFICATION_DATABASE_DSN is required")
	}
	db, err := platformmysql.Open(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("open notification database: %w", err)
	}
	if err := platformmysql.MigrateService(context.Background(), db, "notification"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate notification database: %w", err)
	}
	store := notificationadapter.New(db)
	mqURL := strings.TrimSpace(c.RabbitMQURL)
	if mqURL == "" {
		mqURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	}
	readRetention := time.Duration(c.ReadRetentionDays) * 24 * time.Hour
	if readRetention <= 0 {
		readRetention = 90 * 24 * time.Hour
	}
	unreadRetention := time.Duration(c.UnreadRetentionDays) * 24 * time.Hour
	if unreadRetention <= 0 {
		unreadRetention = 180 * 24 * time.Hour
	}
	jobRuntime, err := xxljob.NewRuntime(c.XXLJob.WithEnv("NOTIFICATION"), nil)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	ctx := &ServiceContext{
		Config: c, Secret: authn.ResolveSecret(c.JWTSecret), MQURL: mqURL, DB: db, Store: store,
		Service: notificationapp.New(store), XXLJob: jobRuntime,
		ReadRetention: readRetention, UnreadRetention: unreadRetention,
	}
	if mqURL != "" {
		ctx.Consumer = mq.NewConsumer(mqURL, "notification.task-events",
			"task.created.v1", "task.completed.v1", "task.cancelled.v1")
	}
	return ctx, nil
}

func (s *ServiceContext) Close() error {
	if s == nil {
		return nil
	}
	if s.XXLJob != nil {
		s.XXLJob.Stop()
	}
	if s.Consumer != nil {
		_ = s.Consumer.Close()
	}
	if s.DB == nil {
		return nil
	}
	return s.DB.Close()
}
