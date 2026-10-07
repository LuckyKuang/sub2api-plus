package outboundidentity

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Identity settings must work independently of host tzdata.

	"golang.org/x/text/language"
)

// Ubuntu 24.04 GA, linux 6.8.0-31.31 amd64. These are advertised client
// defaults, not facts about the gateway host. Do not refresh them with versions.
const (
	DefaultOS            = "linux"
	DefaultArch          = "x64"
	DefaultDeviceName    = "ubuntu"
	DefaultKernelRelease = "6.8.0-31-generic"
	DefaultKernelVersion = "#31-Ubuntu SMP PREEMPT_DYNAMIC Sat Apr 20 00:40:06 UTC 2024"
	DefaultLanguage      = "en-US"
	DefaultTimezone      = "UTC"
)

func ValidateLanguage(value string) error {
	tag, err := language.Parse(value)
	if err != nil || tag.String() != value || value == "und" {
		return fmt.Errorf("language must be a canonical BCP 47 tag, such as en-US or zh-CN")
	}
	return nil
}

func ValidateTimezone(value string) error {
	if value == "" || value == "Local" || (value != "UTC" && !strings.Contains(value, "/")) {
		return fmt.Errorf("timezone must be an IANA name, such as UTC or Asia/Shanghai")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("invalid IANA timezone")
	}
	return nil
}

// TimezoneOffset uses the selected snapshot and the request instant, including
// DST, without consulting the server's TZ. Empty snapshots use the pinned UTC.
func (i Identity) TimezoneOffset(at time.Time) int {
	zone := i.Timezone
	if zone == "" {
		zone = DefaultTimezone
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	_, offset := at.In(location).Zone()
	return offset
}
