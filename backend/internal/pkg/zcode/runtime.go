package zcode

import "github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"

// RuntimeHeaders retains the official header formats using the fixed Ubuntu
// client environment. Explicit global/account declarations remain authoritative.
func RuntimeHeaders() map[string]string {
	return map[string]string{
		"X-Platform": "linux-x64", "X-Os-Category": "linux",
		"X-Os-Version":      outboundidentity.DefaultKernelRelease,
		"X-Client-Language": outboundidentity.DefaultLanguage,
		"X-Client-Timezone": outboundidentity.DefaultTimezone,
	}
}

// The official control plane uses os.version(), inference uses os.release().
func controlRuntimeHeaders() map[string]string {
	return map[string]string{"X-Os-Version": outboundidentity.DefaultKernelVersion}
}
