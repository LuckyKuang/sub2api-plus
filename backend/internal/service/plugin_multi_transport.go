package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type pluginTransportRoute struct {
	pluginID    int64
	bindingID   int64
	runtime     *pluginRuntime
	platform    string
	accountType string
	rollout     int
	unavailable string
}

func (m *PluginManager) reconcileMultiTransport(ctx context.Context, installations []*PluginInstallation) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.multiRuntimes == nil {
		m.multiRuntimes = make(map[int64]*pluginRuntime)
	}
	m.mu.Unlock()
	active := make(map[int64]struct{})
	for _, installation := range installations {
		if installation == nil || !hasEnabledMultiTransportBinding(installation.Bindings) {
			continue
		}
		active[installation.ID] = struct{}{}
		m.mu.Lock()
		current := m.multiRuntimes[installation.ID]
		m.mu.Unlock()
		if current != nil && !current.client.Exited() &&
			current.installation.BinarySHA256 == installation.BinarySHA256 &&
			current.installation.ConfigEncrypted == installation.ConfigEncrypted {
			continue
		}
		local, err := m.ensureLocalInstallation(ctx, installation)
		if err != nil {
			slog.Warn("plugin_multi_transport_install_failed", "plugin_id", installation.ID, "error", err)
			continue
		}
		runtime, err := m.prepareRuntime(ctx, local, true)
		if err != nil {
			slog.Warn("plugin_multi_transport_start_failed", "plugin_id", installation.ID, "error", err)
			continue
		}
		m.mu.Lock()
		previous := m.multiRuntimes[installation.ID]
		m.multiRuntimes[installation.ID] = runtime
		m.mu.Unlock()
		if previous != nil {
			previous.drain(10 * time.Second)
		}
	}

	m.mu.Lock()
	var stale []*pluginRuntime
	for id, runtime := range m.multiRuntimes {
		if _, ok := active[id]; ok {
			continue
		}
		runtime.draining.Store(true)
		stale = append(stale, runtime)
		delete(m.multiRuntimes, id)
	}
	m.mu.Unlock()
	for _, runtime := range stale {
		runtime.drain(10 * time.Second)
	}
	m.publishMultiTransportRoutes(installations)
}

func (m *PluginManager) publishMultiTransportRoutes(installations []*PluginInstallation) {
	routes := make([]pluginTransportRoute, 0)
	counts := make(map[string]int)
	for _, installation := range installations {
		if installation == nil {
			continue
		}
		m.mu.Lock()
		runtime := m.multiRuntimes[installation.ID]
		m.mu.Unlock()
		for _, binding := range installation.Bindings {
			if !binding.Enabled || binding.Capability != PluginCapabilityOutboundTransport || binding.RolloutPercent < 1 {
				continue
			}
			route := pluginTransportRoute{
				pluginID:    installation.ID,
				bindingID:   binding.ID,
				runtime:     runtime,
				platform:    binding.Platform,
				accountType: binding.AccountType,
				rollout:     binding.RolloutPercent,
			}
			if runtime == nil || runtime.client.Exited() {
				route.runtime = nil
				route.unavailable = "插件进程未运行"
			}
			scope := binding.Platform + "/" + binding.AccountType
			counts[scope]++
			routes = append(routes, route)
		}
	}
	for index := range routes {
		if counts[routes[index].platform+"/"+routes[index].accountType] > 1 {
			routes[index].unavailable = "同一平台和账号类型存在多个启用的传输插件"
		}
	}
	m.transportRoutes.Store(routes)
}

func (m *PluginManager) enableMultiTransportPlugin(ctx context.Context, installation *PluginInstallation, compatibility PluginCompatibility, rolloutPercent int) (*PluginInstallation, error) {
	if installation == nil {
		return nil, errors.New("插件安装记录为空")
	}
	m.mu.Lock()
	if m.multiRuntimes == nil {
		m.multiRuntimes = make(map[int64]*pluginRuntime)
	}
	m.mu.Unlock()
	for _, binding := range installation.Bindings {
		if binding.Capability != PluginCapabilityOutboundTransport {
			continue
		}
		if routes, _ := m.transportRoutes.Load().([]pluginTransportRoute); len(routes) > 0 {
			for _, route := range routes {
				if route.pluginID != installation.ID && route.platform == binding.Platform && route.accountType == binding.AccountType {
					return nil, fmt.Errorf("平台 %s/%s 已有启用的出站传输插件，请先停用", binding.Platform, binding.AccountType)
				}
			}
		}
	}
	compatibility = EvaluatePluginCompatibility(installation.Manifest, m.hostInfo)
	bindings := append([]PluginBinding(nil), installation.Bindings...)
	for index := range bindings {
		switch bindings[index].Capability {
		case PluginCapabilityOutboundTransport, PluginCapabilityRequestInstructions:
			bindings[index].Enabled = true
			bindings[index].RolloutPercent = rolloutPercent
		}
	}
	installation, err := m.ensureLocalInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	if err := m.repo.BeginEnable(ctx, installation.ID, installation.BinarySHA256, installation.State); err != nil {
		return nil, err
	}
	runtime, err := m.prepareRuntime(ctx, installation, true)
	if err != nil {
		stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		stateErr := m.repo.UpdateState(stateCtx, installation.ID, PluginStateError, err.Error(), nil, installation.BinarySHA256, PluginStateStarting)
		cancel()
		return nil, errors.Join(err, stateErr)
	}
	now := time.Now()
	if err := m.repo.UpdateBindingsAndState(ctx, installation.ID, bindings, PluginStateEnabled, "", &now, PluginStateStarting, installation.BinarySHA256); err != nil {
		runtime.kill()
		return nil, err
	}
	m.mu.Lock()
	previous := m.multiRuntimes[installation.ID]
	m.multiRuntimes[installation.ID] = runtime
	m.mu.Unlock()
	if previous != nil {
		previous.drain(10 * time.Second)
	}
	installations, listErr := m.repo.List(ctx)
	if listErr == nil {
		m.publishMultiTransportRoutes(installations)
		m.publishRequestRules(installations)
	}
	result, err := m.repo.GetByID(ctx, installation.ID)
	if err != nil {
		return nil, err
	}
	result.Compatibility = compatibility
	result.RuntimeHealthy = true
	result.RuntimeMessage = "插件进程运行中"
	return result, nil
}

// RoundTripTransport selects at most one enabled transport by the actual
// credential-owner platform and account type. Existing v1 OpenAI OAuth routing
// remains the fallback so installed packages keep their behavior.
func (m *PluginManager) RoundTripTransport(ctx context.Context, request *http.Request, proxyURL string, account *Account) (*http.Response, bool, error) {
	if m == nil || account == nil {
		return nil, false, nil
	}
	routes, _ := m.transportRoutes.Load().([]pluginTransportRoute)
	accountType := pluginAccountType(account.Type)
	for _, route := range routes {
		if route.platform != account.Platform || route.accountType != accountType {
			continue
		}
		if int(stablePluginBucket(account.ID)) >= route.rollout {
			return nil, false, nil
		}
		if route.runtime == nil || route.unavailable != "" {
			return nil, true, fmt.Errorf("插件传输不可用: %s", route.unavailable)
		}
		if route.runtime.client.Exited() {
			return nil, true, errors.New("插件进程已退出")
		}
		if !route.runtime.beginRequest() {
			return nil, true, errors.New("插件正在停止")
		}
		response, err := route.runtime.roundTrip(ctx, request, proxyURL, account)
		if err != nil {
			route.runtime.finishRequest()
			return nil, true, err
		}
		return response, true, nil
	}
	return m.RoundTripOpenAIOAuth(ctx, request, proxyURL, account)
}

// ShouldRouteTransport reports a multi-platform transport hit. Callers that
// switch WebSocket ingress to an HTTP bridge must still verify the provider's
// bridge support separately.
func (m *PluginManager) ShouldRouteTransport(account *Account) bool {
	if m == nil || account == nil {
		return false
	}
	routes, _ := m.transportRoutes.Load().([]pluginTransportRoute)
	accountType := pluginAccountType(account.Type)
	for _, route := range routes {
		if route.platform == account.Platform && route.accountType == accountType &&
			int(stablePluginBucket(account.ID)) < route.rollout {
			return true
		}
	}
	return m.ShouldRouteOpenAIOAuth(account)
}
