//go:build !darwin && !linux

package codexcli

import (
	"context"
	"errors"
)

func acquireInstallFileLock(context.Context, string) (func(), error) {
	return nil, errors.New("CLI installation locks are unsupported on this platform")
}
