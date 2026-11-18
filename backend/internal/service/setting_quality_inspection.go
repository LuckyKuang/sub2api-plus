package service

import "context"

// ModelQualityInspectionEnabled 只读取总开关，避免每个管理请求都加载全量设置。
//
// 与 GetChannelMonitorRuntime 同样走 GetMultiple：守卫在高频路径上，不能触发
// 全量设置的解析与密钥解密。
func (s *SettingService) ModelQualityInspectionEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyModelQualityInspectionEnabled})
	if err != nil {
		return false
	}
	return isTrueSettingValue(values[SettingKeyModelQualityInspectionEnabled])
}
