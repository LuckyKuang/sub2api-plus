package kimi

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// DeviceFacts are the host facts the official client reads from the machine it
// runs on and renders into the `X-Msh-*` device set. They are runtime values,
// not compile-time pins: the official client derives them per host and persists
// the device id instead of shipping any of them in the package.
type DeviceFacts struct {
	// Name is `os.hostname()`.
	Name string
	// Model is `deviceModel()`: the OS type, its version and the Node
	// architecture token, mirroring `os.type() / release() / arch()`.
	Model string
	// OSVersion is `os.release()`: the kernel release of the running host.
	OSVersion string
	// DeviceID is the persisted per-install uuid v4.
	DeviceID string
}

// ResolveDeviceFacts reads the host facts. They are read once per process — the
// machine this process runs on does not change while it runs — so a per-request
// resolution never touches the filesystem.
func ResolveDeviceFacts() DeviceFacts {
	deviceFactsOnce.Do(func() {
		deviceFactsValue = readDeviceFacts()
	})
	return deviceFactsValue
}

func readDeviceFacts() DeviceFacts {
	return DeviceFacts{
		Name:      hostName(),
		Model:     deviceModel(),
		OSVersion: osRelease(),
		DeviceID:  DefaultDeviceID(),
	}
}

var (
	deviceFactsOnce  sync.Once
	deviceFactsValue DeviceFacts
)

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

func hostName() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// kernelReleasePath is the file `uname -r` reports. Linux is the platform every
// supported deployment and every validation container runs on.
const kernelReleasePath = "/proc/sys/kernel/osrelease"

// osRelease mirrors `os.release()`. A host that does not expose a release
// through this file falls back to the official `unknown` declaration rather
// than an invented one; an operator can always override the value explicitly.
func osRelease() string {
	release, err := os.ReadFile(kernelReleasePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(release))
}

// macSystemVersionPlist is the file `sw_vers -productVersion` reads, parsed
// directly so a resolution never spawns a child process.
const macSystemVersionPlist = "/System/Library/CoreServices/SystemVersion.plist"

// deviceModel mirrors the official `deviceModel()`: `macOS <product> <arch>` on
// Darwin, `Windows <release> <arch>` on Windows and `<type> <release> <arch>`
// elsewhere. A component the host does not expose is skipped instead of being
// rendered as an empty segment, so a partially resolved host still declares a
// coherent value; a fully unresolved host falls back to `unknown`.
func deviceModel() string {
	osType := hostOSType()
	osVersion := osRelease()
	if osType == "Darwin" {
		if product := macProductVersion(); product != "" {
			osVersion = product
		}
	}
	return joinDeviceModel(osType, osVersion, nodeArch())
}

// joinDeviceModel skips a component the host does not expose instead of
// rendering an empty segment, so a partially resolved host still declares a
// coherent value. A fully unresolved host yields an empty value, which the
// header sanitizer turns into the official `unknown` fallback.
func joinDeviceModel(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// hostOSType mirrors `os.type()`, which reports the platform name `uname(3)`
// returns rather than Go's GOOS token.
func hostOSType() string {
	switch runtime.GOOS {
	case "darwin":
		return "Darwin"
	case "windows":
		return "Windows_NT"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	case "solaris", "illumos":
		return "SunOS"
	case "aix":
		return "AIX"
	case "android":
		return "Android"
	default:
		return runtime.GOOS
	}
}

// nodeArch mirrors `os.arch()`, which reports the architecture token rather
// than the GOARCH name: `amd64` hosts advertise `x64`, `386` hosts `ia32`.
func nodeArch() string {
	return nodeArchFor(runtime.GOARCH)
}

func nodeArchFor(arch string) string {
	switch arch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	case "mipsle":
		return "mipsel"
	default:
		return arch
	}
}

var macProductVersionPattern = regexp.MustCompile(`<key>ProductVersion</key>\s*<string>([^<]*)</string>`)

// macProductVersion mirrors `macOsProductVersion()`: the macOS product version
// (`15.3.1`) rather than the Darwin kernel release (`24.3.0`).
func macProductVersion() string {
	plist, err := os.ReadFile(macSystemVersionPlist)
	if err != nil {
		return ""
	}
	matches := macProductVersionPattern.FindStringSubmatch(string(plist))
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}
