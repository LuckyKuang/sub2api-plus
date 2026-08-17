package service

import (
	"strings"
	"time"
)

func buildObjectStorageKey(prefix, fallback string, appendDatePath bool, at time.Time, objectName string) string {
	base := strings.TrimRight(strings.TrimSpace(prefix), "/")
	if base == "" {
		base = fallback
	}
	if appendDatePath {
		base += "/" + at.Format("2006/01/02")
	}
	return base + "/" + strings.TrimLeft(objectName, "/")
}
