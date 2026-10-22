//go:build unit || !integration

package zcode

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceIdentityFailureAndPinPrecedence(t *testing.T) {
	deviceIdentity.RLock()
	previous := deviceIdentity.resolve
	deviceIdentity.RUnlock()
	t.Cleanup(func() { SetDeviceIDResolver(previous) })
	SetDeviceIDResolver(nil)
	_, err := EnsureDeviceIdentity(context.Background(), DefaultIdentity())
	require.Error(t, err)
	failed := errors.New("persistent device store unavailable")
	SetDeviceIDResolver(func(context.Context) (string, error) { return "", failed })
	_, err = EnsureDeviceIdentity(context.Background(), DefaultIdentity())
	require.ErrorIs(t, err, failed)
	for _, invalid := range []string{"", "not-a-device", "3121f5a2-39af-756a-a7bc-38dc58cfd3af"} {
		SetDeviceIDResolver(func(context.Context) (string, error) { return invalid, nil })
		_, err = EnsureDeviceIdentity(context.Background(), DefaultIdentity())
		require.Error(t, err)
	}
	pinned := DefaultIdentity()
	pinned.ControlHeaders[HeaderDeviceMid] = "7d918214-79ad-4057-bb2b-4ac6bd24a907"
	got, err := EnsureDeviceIdentity(context.Background(), pinned)
	require.NoError(t, err)
	require.Equal(t, "7d918214-79ad-4057-bb2b-4ac6bd24a907", got.ControlHeaders[HeaderDeviceMid])
	require.Empty(t, got.Headers[HeaderDeviceMid])
}
