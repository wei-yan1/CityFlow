// Package taskmysql is the Task service's MySQL adapter. It owns tasks,
// task events/incidents, riders, rider applications, feedback and the task
// outbox. It never reads identity, merchant or payment tables.
package taskmysql

import (
	"crypto/rand"
	"fmt"

	"cityflow/app/task/repository"
	"cityflow/internal/common/domain"
	platformmysql "cityflow/internal/platform/mysql"
)

type Store struct {
	db *platformmysql.DB
}

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

var (
	_ repository.TaskRepository  = (*Store)(nil)
	_ repository.RiderRepository = (*Store)(nil)
	_ repository.RiderFeedback   = (*Store)(nil)
)

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := fmt.Sprintf("%x", raw)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

var _ = domain.ErrNotFound
