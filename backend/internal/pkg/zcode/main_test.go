//go:build unit || !integration

package zcode

import (
	"context"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	SetDeviceIDResolver(func(context.Context) (string, error) { return "c9cfbfa7-e7c9-4cec-8b1e-e11fa92c32f2", nil })
	os.Exit(m.Run())
}
