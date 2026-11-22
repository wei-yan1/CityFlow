package comment

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cityflow/internal/common/domain"
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

// EncodeHotCursor carries the three sort keys of the hot list:
// like_count, created_at, and id. The "hot" marker prevents a latest cursor
// from being accepted accidentally.
func EncodeHotCursor(likeCount int, createdAt time.Time, id string) string {
	raw := "hot|" + strconv.Itoa(likeCount) + "|" + createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeHotCursor(cursor string) (int, time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	parts := strings.SplitN(string(raw), "|", 4)
	if len(parts) != 4 || parts[0] != "hot" || parts[3] == "" {
		return 0, time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	likeCount, err := strconv.Atoi(parts[1])
	if err != nil || likeCount < 0 {
		return 0, time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	value, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil {
		return 0, time.Time{}, "", fmt.Errorf("%w: 评论游标无效", domain.ErrInvalidRequest)
	}
	return likeCount, value, parts[3], nil
}
