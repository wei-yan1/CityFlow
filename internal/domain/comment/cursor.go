package comment

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/domain"
)

// EncodeCursor makes the created_at/id cursor opaque to clients.
func EncodeCursor(createdAt time.Time, id string) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	value, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	return value, parts[1], nil
}
