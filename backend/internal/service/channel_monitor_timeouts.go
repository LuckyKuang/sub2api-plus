package service

import (
	"strconv"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

const (
	monitorDefaultResponseHeaderTimeoutSeconds = 30
	monitorDefaultTotalTimeoutSeconds          = 45
	monitorMaxProbeTimeoutSeconds              = 120
	monitorMaxCheckTimeout                     = monitorMaxProbeTimeoutSeconds*time.Second + monitorPingTimeout + monitorQuotaFetchTimeout + monitorRunOneBuffer
)

type channelMonitorProbeTimeouts struct {
	responseHeaderSeconds int
	totalSeconds          int
}

func ValidateChannelMonitorProbeTimeouts(responseHeaderSeconds, totalSeconds int) error {
	if responseHeaderSeconds < 1 || responseHeaderSeconds > monitorMaxProbeTimeoutSeconds ||
		totalSeconds < 1 || totalSeconds > monitorMaxProbeTimeoutSeconds || responseHeaderSeconds > totalSeconds {
		return infraerrors.BadRequest("INVALID_CHANNEL_MONITOR_PROBE_TIMEOUTS", "V1 probe timeouts must be between 1 and 120 seconds, and the response-header timeout must not exceed the total timeout")
	}
	return nil
}

func readChannelMonitorProbeTimeouts(values map[string]string) channelMonitorProbeTimeouts {
	responseHeaderSeconds := monitorDefaultResponseHeaderTimeoutSeconds
	totalSeconds := monitorDefaultTotalTimeoutSeconds
	if raw := values[SettingKeyChannelMonitorProbeResponseHeaderTimeoutSeconds]; raw != "" {
		responseHeaderSeconds, _ = strconv.Atoi(raw)
	}
	if raw := values[SettingKeyChannelMonitorProbeTotalTimeoutSeconds]; raw != "" {
		totalSeconds, _ = strconv.Atoi(raw)
	}
	if ValidateChannelMonitorProbeTimeouts(responseHeaderSeconds, totalSeconds) != nil {
		return channelMonitorProbeTimeouts{monitorDefaultResponseHeaderTimeoutSeconds, monitorDefaultTotalTimeoutSeconds}
	}
	return channelMonitorProbeTimeouts{responseHeaderSeconds, totalSeconds}
}

func (t channelMonitorProbeTimeouts) checkTimeout(checkMode string) time.Duration {
	if checkMode == MonitorCheckModeQuota {
		// Keep the original 63-second round budget for quota-only monitors.
		return monitorQuotaFetchTimeout + monitorPingTimeout + monitorRunOneBuffer
	}
	budget := time.Duration(t.totalSeconds)*time.Second + monitorPingTimeout + monitorRunOneBuffer
	if checkMode == MonitorCheckModeQuotaProbe {
		budget += monitorQuotaFetchTimeout
	}
	return budget
}
