package zcode

import (
	"maps"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// RuntimeHeaders mirrors bootstrap/runtime-platform-headers.ts and the Intl
// declarations in bootstrap/model-config.ts. Settings persist these host facts;
// product version changes never regenerate them.
func RuntimeHeaders() map[string]string {
	runtimeOnce.Do(func() {
		release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
		locale := ""
		for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if locale = strings.TrimSpace(os.Getenv(name)); locale != "" {
				break
			}
		}
		locale, _, _ = strings.Cut(locale, ".")
		locale, _, _ = strings.Cut(locale, "@")
		if locale == "" || locale == "C" || locale == "POSIX" {
			locale = "en-US"
		}
		locale = strings.ReplaceAll(locale, "_", "-")
		zone := time.Local.String()
		if configured := strings.TrimSpace(os.Getenv("TZ")); configured != "" {
			zone = configured
		}
		if zone == "Local" {
			if data, err := os.ReadFile("/etc/timezone"); err == nil {
				zone = strings.TrimSpace(string(data))
			}
			if path, err := os.Readlink("/etc/localtime"); err == nil {
				if _, name, ok := strings.Cut(path, "/zoneinfo/"); ok {
					zone = name
				}
			}
		}
		if zone == "Local" {
			zone = "unknown"
		}
		runtimeValues = runtimeHeaders(runtime.GOOS, runtime.GOARCH, string(release), locale, zone)
	})
	return maps.Clone(runtimeValues)
}

var runtimeOnce sync.Once
var runtimeValues map[string]string

func runtimeHeaders(platform, arch, release, locale, zone string) map[string]string {
	category := "linux"
	switch platform {
	case "darwin":
		category = "macos"
	case "windows":
		platform, category = "win32", "windows"
	}
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "ia32"
	case "mipsle":
		arch = "mipsel"
	}
	return map[string]string{
		"X-Platform":        printableFact(platform + "-" + arch),
		"X-Os-Category":     category,
		"X-Os-Version":      printableFact(release),
		"X-Client-Language": printableFact(locale),
		"X-Client-Timezone": printableFact(zone),
	}
}

func printableFact(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return "unknown"
	}
	for _, c := range value {
		if c < 32 || c > 126 {
			return "unknown"
		}
	}
	return value
}
