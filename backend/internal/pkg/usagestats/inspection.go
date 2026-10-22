package usagestats

import (
	"errors"
	"strconv"
)

// ParseSnapshotMaxID accepts an explicit insertion boundary or requests a new one.
func ParseSnapshotMaxID(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	if raw == "new" {
		value := int64(-1)
		return &value, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return nil, errors.New("invalid snapshot_max_id")
	}
	return &value, nil
}
