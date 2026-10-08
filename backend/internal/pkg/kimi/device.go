package kimi

import (
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

// DeviceFacts use the official declaration formats. Defaults describe a fixed
// Ubuntu client, never the gateway's hostname, kernel or CPU architecture.
type DeviceFacts struct {
	// Name is the advertised client hostname.
	Name string
	// Model is `deviceModel()`: the OS type, its version and the Node
	// architecture token, mirroring `os.type() / release() / arch()`.
	Model string
	// OSVersion is `os.release()`: the advertised client kernel release.
	OSVersion string
	// DeviceID is the persisted per-install uuid v4.
	DeviceID string
}

// ResolveDeviceFacts returns the fixed Ubuntu client defaults and a stable device ID.
func ResolveDeviceFacts() DeviceFacts {
	return DeviceFacts{
		Name:      outboundidentity.DefaultDeviceName,
		Model:     "Linux " + outboundidentity.DefaultKernelRelease + " " + outboundidentity.DefaultArch,
		OSVersion: outboundidentity.DefaultKernelRelease,
		DeviceID:  DefaultDeviceID(),
	}
}

// DeviceDeclarations renders the device facts as the four headers the official
// client sends next to its User-Agent.
func DeviceDeclarations(facts DeviceFacts) map[string]string {
	deviceID := strings.TrimSpace(facts.DeviceID)
	if deviceID == "" {
		deviceID = DefaultDeviceID()
	}
	return map[string]string{
		HeaderDeviceName:  asciiHeader(facts.Name),
		HeaderDeviceModel: asciiHeader(facts.Model),
		HeaderOSVersion:   asciiHeader(facts.OSVersion),
		HeaderDeviceID:    deviceID,
	}
}

// DefaultDeviceID returns the device id this process advertises when no
// persisted or configured value exists. It is minted once per process so one
// resolution never produces two different ids for the same deployment snapshot.
func DefaultDeviceID() string {
	defaultDeviceIDOnce.Do(func() {
		defaultDeviceIDValue = NewDeviceID()
	})
	return defaultDeviceIDValue
}

// NewDeviceID mints a fresh canonical uuid v4, matching the value the official
// `randomUUID()` call persists.
func NewDeviceID() string {
	return uuid.NewString()
}

var (
	defaultDeviceIDOnce  sync.Once
	defaultDeviceIDValue string
)

// asciiHeader mirrors the official header sanitizer: printable ASCII only,
// trimmed, with the `unknown` fallback when nothing survives.
func asciiHeader(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7E {
			return -1
		}
		return r
	}, value)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return unknownFact
	}
	return cleaned
}
