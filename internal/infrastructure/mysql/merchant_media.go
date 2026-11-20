package mysqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func (s *Store) GetMerchantMedia(ctx context.Context, id string) (string, []string, error) {
	var avatar string
	var raw []byte
	e := s.db.QueryRowContext(ctx, `SELECT avatar_url,photos FROM merchant_media WHERE merchant_id=?`, id).Scan(&avatar, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil, nil
	}
	if e != nil {
		return "", nil, e
	}
	var images []string
	if e = json.Unmarshal(raw, &images); e != nil {
		return "", nil, e
	}
	return avatar, images, nil
}
func (s *Store) SaveMerchantMedia(ctx context.Context, id, avatar string, images []string) error {
	raw, e := json.Marshal(images)
	if e != nil {
		return e
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO merchant_media(merchant_id,avatar_url,photos) VALUES(?,?,?) ON DUPLICATE KEY UPDATE avatar_url=VALUES(avatar_url),photos=VALUES(photos)`, id, avatar, raw)
	return e
}
