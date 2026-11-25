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

func (m *PluginManager) dropMultiRuntime(id int64, message string) {
	m.mu.Lock()
	runtime := m.multiRuntimes[id]
	delete(m.multiRuntimes, id)
	if m.multiUnavailable == nil {
		m.multiUnavailable = make(map[int64]string)
	}
	m.multiUnavailable[id] = message
	m.mu.Unlock()
	if runtime != nil {
		runtime.drain(10 * time.Second)
	}
}

func (m *PluginManager) reconcileMultiTransport(ctx context.Context, installations []*PluginInstallation) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.multiRuntimes == nil {
		m.multiRuntimes = make(map[int64]*pluginRuntime)
	}
	if m.multiUnavailable == nil {
		m.multiUnavailable = make(map[int64]string)
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
		unavailable := m.multiUnavailable[installation.ID]
		m.mu.Unlock()
		if current != nil && !current.client.Exited() &&
			current.installation.BinarySHA256 == installation.BinarySHA256 &&
			current.installation.ConfigEncrypted == installation.ConfigEncrypted &&
			unavailable == "" {
			healthCtx, cancel := context.WithTimeout(ctx, pluginHealthTimeout)
			healthErr := current.checkHealth(healthCtx)
			cancel()
			if healthErr == nil {
				continue
			}
			m.dropMultiRuntime(installation.ID, "插件健康检查失败: "+healthErr.Error())
		}
		local, err := m.ensureLocalInstallation(ctx, installation)
		if err != nil {
			slog.Warn("plugin_multi_transport_install_failed", "plugin_id", installation.ID, "error", err)
			m.dropMultiRuntime(installation.ID, "插件安装恢复失败: "+err.Error())
			continue
		}
		runtime, err := m.prepareRuntime(ctx, local, true)
		if err != nil {
			slog.Warn("plugin_multi_transport_start_failed", "plugin_id", installation.ID, "error", err)
			m.dropMultiRuntime(installation.ID, "插件启动失败: "+err.Error())
			continue
		}
		// The binding may have changed while the runtime was starting. Never
		// publish a runtime prepared from a superseded snapshot.
		latest, err := m.repo.GetByID(ctx, installation.ID)
		if err != nil || latest == nil || !hasEnabledMultiTransportBinding(latest.Bindings) ||
			latest.BinarySHA256 != installation.BinarySHA256 ||
			latest.ConfigEncrypted != installation.ConfigEncrypted {
			runtime.kill()
			m.dropMultiRuntime(installation.ID, "安装状态已变化，等待重新确认")
			continue
		}
		m.mu.Lock()
		previous := m.multiRuntimes[installation.ID]
		delete(m.multiUnavailable, installation.ID)
		m.multiRuntimes[installation.ID] = runtime
		m.mu.Unlock()
		if previous != nil && previous != runtime {
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
	for id := range m.multiUnavailable {
		if _, ok := active[id]; !ok {
			delete(m.multiUnavailable, id)
		}
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
	m.mu.Lock()
	unavailableById := make(map[int64]string, len(m.multiUnavailable))
	for id, message := range m.multiUnavailable {
		unavailableById[id] = message
	}
	m.mu.Unlock()
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
			if message := unavailableById[installation.ID]; message != "" {
				route.runtime = nil
				route.unavailable = message
			} else if runtime == nil || runtime.client.Exited() {
				route.runtime = nil
				route.unavailable = "插件进程未运行"
			}
			counts[binding.Platform+"/"+binding.AccountType]++
			routes = append(routes, route)
		}
	}
	for index := range routes {
		if counts[routes[index].platform+"/"+routes[index].accountType] > 1 {
			routes[index].runtime = nil
			routes[index].unavailable = "同一平台和账号类型存在多个启用的传输插件"
		}
	}
	m.transportRoutes.Store(routes)
	// This table is only published from an authoritative persisted snapshot.
	m.transportAuthority.Store(true)
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
		m.dropMultiRuntime(installation.ID, "插件启动失败: "+err.Error())
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
	delete(m.multiUnavailable, installation.ID)
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
	// A mixed package may also declare the legacy OpenAI OAuth transport. Start
	// that runtime now instead of leaving the binding enabled but unrouted until
	// a later reconciliation.
	if manifestHasOpenAIOAuthCapability(installation.Manifest) {
		v1Runtime, v1Err := m.prepareRuntime(ctx, installation, true)
		if v1Err != nil {
			m.publishUnavailableRoute(installation.ID, rolloutPercent, "OpenAI OAuth 传输启动失败: "+v1Err.Error())
		} else {
			m.mu.Lock()
			previous := m.runtimes[installation.ID]
			m.runtimes[installation.ID] = v1Runtime
			m.route.Store(&pluginRoute{pluginID: installation.ID, runtime: v1Runtime, rolloutPercent: rolloutPercent})
			m.mu.Unlock()
			if previous != nil && previous != v1Runtime {
				previous.drain(10 * time.Second)
			}
		}
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
	if !m.transportAuthority.Load() {
		return nil, true, errors.New("插件传输路由暂时无法读取")
	}
	routes, _ := m.transportRoutes.Load().([]pluginTransportRoute)
	accountType := pluginAccountType(account.Type)
	for _, route := range routes {
		if route.platform != account.Platform || route.accountType != accountType {
			continue
		}
		if int(stablePluginBucket(account.ID)) >= route.rollout {
			// A v2 rollout miss falls through to another matching route or the
			// existing v1 transport, never straight to the native sender.
			continue
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
	if !m.transportAuthority.Load() {
		// Unknown authority must not be reported as "no plugin binding".
		return true
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
