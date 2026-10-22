package mcpserver

import (
	"encoding/base64"
	"fmt"
	"strconv"
)

// pageItems applies an opaque offset cursor and a bounded page size to a
// sorted list. The cursor is intentionally opaque; callers must not construct
// it or assume that an offset remains valid after the underlying list changes.
func pageItems[T any](items []T, cursor string, limit, defaultLimit, maxLimit int) ([]T, string, error) {
	offset := 0
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		offset, err = strconv.Atoi(string(raw))
		if err != nil || offset < 0 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset > len(items) {
		return nil, "", fmt.Errorf("cursor is out of range")
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[offset:end]
	next := ""
	if end < len(items) {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	return page, next, nil
}
