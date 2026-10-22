package zcode

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/google/uuid"
)

const HeaderDeviceMid = "X-Device-Mid"

var deviceIdentity struct {
	sync.RWMutex
	resolve func(context.Context) (string, error)
}

// SetDeviceIDResolver installs the application-owned persistent installation
// store. Tests and standalone package consumers may supply their own store.
func SetDeviceIDResolver(resolve func(context.Context) (string, error)) {
	deviceIdentity.Lock()
	defer deviceIdentity.Unlock()
	deviceIdentity.resolve = resolve
}

func EnsureDeviceIdentity(ctx context.Context, identity outboundidentity.Identity) (outboundidentity.Identity, error) {
	if identity.ControlHeaders[HeaderDeviceMid] != "" {
		return identity, nil
	}
	deviceIdentity.RLock()
	resolve := deviceIdentity.resolve
	deviceIdentity.RUnlock()
	id := ""
	if resolve != nil {
		var err error
		id, err = resolve(ctx)
		if err != nil {
			return identity, err
		}
	} else {
		return identity, errors.New("ZCode device identity store is unavailable")
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 || parsed.String() != id {
		return identity, errors.New("ZCode device identity store returned an invalid UUIDv4")
	}
	identity.ControlHeaders = maps.Clone(identity.ControlHeaders)
	if identity.ControlHeaders == nil {
		identity.ControlHeaders = map[string]string{}
	}
	identity.ControlHeaders[HeaderDeviceMid] = id
	return identity, nil
}
