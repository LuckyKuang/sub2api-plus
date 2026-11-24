package service

import "strings"

// codexPublicErrorDefinition is the single source of truth for the public
// Codex-facing error catalog. Keys are stable across releases; default text is
// only a fallback and may be overridden per installation.
type codexPublicErrorDefinition struct {
	Key            string
	Group          string
	PublicCode     string
	DefaultMessage string
	Aliases        []string
	ErrorType      string
}

var codexPublicErrorCatalog = []codexPublicErrorDefinition{
	{
		Key:            "missing_required_parameter",
		Group:          "request",
		PublicCode:     "missing_required_parameter",
		DefaultMessage: "请求缺少必填参数，请补全后重新提交。",
		Aliases:        []string{"missing_required_parameter"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_parameter",
		Group:          "request",
		PublicCode:     "invalid_parameter",
		DefaultMessage: "请求参数无效，请检查参数值和格式后重新提交。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "unsupported_parameter",
		Group:          "request",
		PublicCode:     "unsupported_parameter",
		DefaultMessage: "当前模型不支持所选请求选项，请调整选项或更换模型。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_prompt",
		Group:          "request",
		PublicCode:     "invalid_prompt",
		DefaultMessage: "输入内容无效，请检查后重新提交。",
		Aliases:        []string{"invalid_prompt"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "context_length_exceeded",
		Group:          "request",
		PublicCode:     "context_length_exceeded",
		DefaultMessage: "会话内容超出模型的上下文长度，请精简历史或新开会话。",
		Aliases:        []string{"context_length_exceeded", "context_too_large"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "request_too_large",
		Group:          "request",
		PublicCode:     "request_too_large",
		DefaultMessage: "请求数据过大，请减少输入内容或附件后重新提交。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "budget_reached",
		Group:          "request",
		PublicCode:     "budget_reached",
		DefaultMessage: "当前会话已达到预算上限，请检查会话预算设置。",
		Aliases:        []string{"budget_reached"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "model_not_found",
		Group:          "request",
		PublicCode:     "model_not_found",
		DefaultMessage: "所选模型当前不可用，请更换模型。",
		Aliases:        []string{"model_not_found", "model_not_available", "unsupported_model", "invalid_model"},
		ErrorType:      "not_found_error",
	},
	{
		Key:            "invalid_access_program",
		Group:          "request",
		PublicCode:     "invalid_access_program",
		DefaultMessage: "当前模型不可使用所选服务选项，请调整选项或更换模型。",
		Aliases:        []string{"invalid_access_program", "unsupported_access_program", "access_program_not_enabled"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "unsupported_access_program",
		Group:          "request",
		PublicCode:     "unsupported_access_program",
		DefaultMessage: "当前模型不可使用所选服务选项，请调整选项或更换模型。",
		Aliases:        []string{"invalid_access_program", "unsupported_access_program", "access_program_not_enabled"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "access_program_not_enabled",
		Group:          "request",
		PublicCode:     "access_program_not_enabled",
		DefaultMessage: "当前模型不可使用所选服务选项，请调整选项或更换模型。",
		Aliases:        []string{"invalid_access_program", "unsupported_access_program", "access_program_not_enabled"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "service_tier_unavailable",
		Group:          "request",
		PublicCode:     "service_tier_unavailable",
		DefaultMessage: "当前模型不可使用所选服务选项，请调整选项或更换模型。",
		Aliases:        []string{"invalid_access_program", "unsupported_access_program", "access_program_not_enabled"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "resource_not_found",
		Group:          "request",
		PublicCode:     "resource_not_found",
		DefaultMessage: "引用的资源不存在或已过期，请重新上传或选择资源。",
		Aliases:        []string{},
		ErrorType:      "not_found_error",
	},
	{
		Key:            "previous_response_not_found",
		Group:          "request",
		PublicCode:     "previous_response_not_found",
		DefaultMessage: "找不到上一轮响应，请携带所需历史重新提交，或新开会话。",
		Aliases:        []string{"previous_response_not_found"},
		ErrorType:      "not_found_error",
	},
	{
		Key:            "session_account_lost",
		Group:          "request",
		PublicCode:     "session_account_lost",
		DefaultMessage: "当前会话可能无法恢复，请新开会话继续。",
		Aliases:        []string{"session_account_lost"},
		ErrorType:      "server_error",
	},
	{
		Key:            "invalid_encrypted_content",
		Group:          "request",
		PublicCode:     "invalid_encrypted_content",
		DefaultMessage: "会话历史无法验证，请新开会话并重新提供必要内容。",
		Aliases:        []string{"invalid_encrypted_content", "thinking_signature_invalid"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_tool_history",
		Group:          "request",
		PublicCode:     "invalid_tool_history",
		DefaultMessage: "工具结果与调用记录不匹配，请补全本轮记录或新开会话。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "request_conflict",
		Group:          "request",
		PublicCode:     "request_conflict",
		DefaultMessage: "当前操作与已有请求冲突，请确认其状态后再提交。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_stream_id",
		Group:          "request",
		PublicCode:     "invalid_stream_id",
		DefaultMessage: "流标识无效，请更正后重试。",
		Aliases:        []string{"invalid_stream_id", "websocket_stream_limit_reached", "websocket_connection_limit_reached"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "websocket_stream_limit_reached",
		Group:          "request",
		PublicCode:     "websocket_stream_limit_reached",
		DefaultMessage: "此连接已达到流数量上限，请复用已有流或重新连接。",
		Aliases:        []string{"invalid_stream_id", "websocket_stream_limit_reached", "websocket_connection_limit_reached"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "websocket_connection_limit_reached",
		Group:          "request",
		PublicCode:     "websocket_connection_limit_reached",
		DefaultMessage: "当前连接已到使用时限，请重新连接后继续。",
		Aliases:        []string{"invalid_stream_id", "websocket_stream_limit_reached", "websocket_connection_limit_reached"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "transport_unavailable",
		Group:          "request",
		PublicCode:     "transport_unavailable",
		DefaultMessage: "当前连接方式暂不可用，请改用其他受支持的连接方式或稍后再试。",
		Aliases:        []string{"websocket_not_supported", "websocket_unsupported", "upgrade_required"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "unsupported_media_type",
		Group:          "request",
		PublicCode:     "unsupported_media_type",
		DefaultMessage: "请求的内容格式不受支持，请更换格式后重新提交。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "continuation_unsupported",
		Group:          "request",
		PublicCode:     "continuation_unsupported",
		DefaultMessage: "当前请求方式不支持续接上一轮，请携带所需历史重新提交。",
		Aliases:        []string{"openai_http_continuation_unsupported"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "credential_unavailable",
		Group:          "service",
		PublicCode:     "service_unavailable",
		DefaultMessage: "服务暂不可用，请稍后再试。若持续出现，请联系管理员。",
		Aliases:        []string{"invalid_grant"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "account_access_unavailable",
		Group:          "service",
		PublicCode:     "service_unavailable",
		DefaultMessage: "服务暂不可用，请稍后再试。若持续出现，请联系管理员。",
		Aliases:        []string{"deactivated_workspace"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "service_capacity_unavailable",
		Group:          "service",
		PublicCode:     "service_capacity_unavailable",
		DefaultMessage: "当前服务额度不可用，请联系管理员。",
		Aliases:        []string{"insufficient_quota", "insufficient_balance", "credit_balance_exhausted"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "feature_unavailable",
		Group:          "service",
		PublicCode:     "feature_unavailable",
		DefaultMessage: "当前服务暂不提供此功能，请选择其他可用功能或联系管理员。",
		Aliases:        []string{"usage_not_included"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "service_configuration_unavailable",
		Group:          "service",
		PublicCode:     "service_configuration_unavailable",
		DefaultMessage: "当前服务配置无法处理此请求，请联系管理员。",
		Aliases:        []string{"data_residency_mismatch"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "rate_limit_exceeded",
		Group:          "service",
		PublicCode:     "rate_limit_exceeded",
		DefaultMessage: "当前请求受到频率限制，请稍后再试。",
		Aliases:        []string{"rate_limit_exceeded"},
		ErrorType:      "rate_limit_error",
	},
	{
		Key:            "model_overloaded",
		Group:          "service",
		PublicCode:     "server_error",
		DefaultMessage: "当前模型繁忙，请稍后再试或更换模型。",
		Aliases:        []string{"server_is_overloaded", "slow_down"},
		ErrorType:      "server_error",
	},
	{
		Key:            "policy_capacity_unavailable",
		Group:          "service",
		PublicCode:     "service_unavailable_error",
		DefaultMessage: "当前请求所需的服务容量暂不可用，请稍后再试。",
		Aliases:        []string{"policy_capacity_unavailable"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "flex_unavailable",
		Group:          "service",
		PublicCode:     "flex_unavailable",
		DefaultMessage: "当前服务层级容量不足，请稍后再试或调整服务层级。",
		Aliases:        []string{"flex_unavailable"},
		ErrorType:      "rate_limit_error",
	},
	{
		Key:            "content_policy_violation",
		Group:          "service",
		PublicCode:     "content_policy_violation",
		DefaultMessage: "此请求未通过内容安全检查，请调整提交内容。",
		Aliases:        []string{"content_policy_violation", "image_content_policy_violation", "moderation_blocked"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_content_policy_violation",
		Group:          "service",
		PublicCode:     "image_content_policy_violation",
		DefaultMessage: "此请求未通过内容安全检查，请调整提交内容。",
		Aliases:        []string{"content_policy_violation", "image_content_policy_violation", "moderation_blocked"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "cyber_policy",
		Group:          "service",
		PublicCode:     "cyber_policy",
		DefaultMessage: "此请求受到安全策略限制，无法完成。",
		Aliases:        []string{"cyber_policy", "bio_policy", "misalignment_policy_violation"},
		ErrorType:      "permission_error",
	},
	{
		Key:            "bio_policy",
		Group:          "service",
		PublicCode:     "bio_policy",
		DefaultMessage: "此请求受到安全策略限制，无法完成。",
		Aliases:        []string{"cyber_policy", "bio_policy", "misalignment_policy_violation"},
		ErrorType:      "permission_error",
	},
	{
		Key:            "misalignment_policy_violation",
		Group:          "service",
		PublicCode:     "misalignment_policy_violation",
		DefaultMessage: "此请求受到安全策略限制，无法完成。",
		Aliases:        []string{"cyber_policy", "bio_policy", "misalignment_policy_violation"},
		ErrorType:      "permission_error",
	},
	{
		Key:            "inference_hook_denied",
		Group:          "service",
		PublicCode:     "inference_hook_denied",
		DefaultMessage: "当前访问策略不允许此请求，请联系管理员。",
		Aliases:        []string{"inference_hook_denied"},
		ErrorType:      "permission_error",
	},
	{
		Key:            "request_policy_denied",
		Group:          "service",
		PublicCode:     "request_policy_denied",
		DefaultMessage: "当前访问策略不允许此请求，请联系管理员。",
		Aliases:        []string{"inference_hook_denied"},
		ErrorType:      "permission_error",
	},
	{
		Key:            "service_usage_limit_reached",
		Group:          "service",
		PublicCode:     "service_usage_limit_reached",
		DefaultMessage: "当前服务已达到本周期用量上限，请在额度恢复后重试。",
		Aliases:        []string{"usage_limit_reached"},
		ErrorType:      "rate_limit_error",
	},
	{
		Key:            "service_connection_failed",
		Group:          "transport",
		PublicCode:     "service_connection_failed",
		DefaultMessage: "暂时无法连接服务，请稍后再试。",
		Aliases:        []string{},
		ErrorType:      "server_error",
	},
	{
		Key:            "response_timeout",
		Group:          "transport",
		PublicCode:     "response_timeout",
		DefaultMessage: "等待服务响应超时，请稍后再试。",
		Aliases:        []string{"first_output_timeout", "vector_store_timeout"},
		ErrorType:      "server_error",
	},
	{
		Key:            "vector_store_timeout",
		Group:          "transport",
		PublicCode:     "vector_store_timeout",
		DefaultMessage: "等待服务响应超时，请稍后再试。",
		Aliases:        []string{"first_output_timeout", "vector_store_timeout"},
		ErrorType:      "server_error",
	},
	{
		Key:            "upstream_response_interrupted",
		Group:          "transport",
		PublicCode:     "upstream_response_interrupted",
		DefaultMessage: "本次响应已中断。请先检查已收到的内容，再决定是否继续。",
		Aliases:        []string{"upstream_response_interrupted", "upstream_http2_stream_error", "upstream_stream_read_error", "upstream_stream_truncated", "stream_timeout"},
		ErrorType:      "server_error",
	},
	{
		Key:            "upstream_unavailable",
		Group:          "transport",
		PublicCode:     "upstream_unavailable",
		DefaultMessage: "服务暂时不可用，请稍后再试。",
		Aliases:        []string{"upstream_unavailable"},
		ErrorType:      "service_unavailable_error",
	},
	{
		Key:            "request_outcome_unknown",
		Group:          "transport",
		PublicCode:     "request_outcome_unknown",
		DefaultMessage: "无法确认本次操作是否完成，请先检查执行结果，避免重复操作。",
		Aliases:        []string{},
		ErrorType:      "server_error",
	},
	{
		Key:            "invalid_service_response",
		Group:          "transport",
		PublicCode:     "invalid_service_response",
		DefaultMessage: "服务返回的响应不完整，请稍后再试。",
		Aliases:        []string{"response_too_large"},
		ErrorType:      "server_error",
	},
	{
		Key:            "empty_response",
		Group:          "transport",
		PublicCode:     "empty_response",
		DefaultMessage: "本次请求未获得有效结果，请调整请求或稍后再试。",
		Aliases:        []string{"openai_silent_refusal"},
		ErrorType:      "server_error",
	},
	{
		Key:            "server_error",
		Group:          "transport",
		PublicCode:     "server_error",
		DefaultMessage: "服务处理请求时出错，请稍后再试。",
		Aliases:        []string{"server_error"},
		ErrorType:      "server_error",
	},
	{
		Key:            "request_rejected",
		Group:          "transport",
		PublicCode:     "request_rejected",
		DefaultMessage: "本次请求未被接受，请检查请求内容。",
		Aliases:        []string{},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "service_error",
		Group:          "transport",
		PublicCode:     "service_error",
		DefaultMessage: "本次请求未能完成，请稍后再试。",
		Aliases:        []string{},
		ErrorType:      "server_error",
	},
	{
		Key:            "invalid_image",
		Group:          "media",
		PublicCode:     "invalid_image",
		DefaultMessage: "图片数据无法读取，请重新导出或上传图片。",
		Aliases:        []string{"invalid_image", "invalid_base64_image", "image_parse_error"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_base64_image",
		Group:          "media",
		PublicCode:     "invalid_base64_image",
		DefaultMessage: "图片数据无法读取，请重新导出或上传图片。",
		Aliases:        []string{"invalid_image", "invalid_base64_image", "image_parse_error"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_parse_error",
		Group:          "media",
		PublicCode:     "image_parse_error",
		DefaultMessage: "图片数据无法读取，请重新导出或上传图片。",
		Aliases:        []string{"invalid_image", "invalid_base64_image", "image_parse_error"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_image_format",
		Group:          "media",
		PublicCode:     "invalid_image_format",
		DefaultMessage: "图片格式或颜色模式不受支持，请转换后重新上传。",
		Aliases:        []string{"invalid_image_format", "invalid_image_mode", "unsupported_image_media_type"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_image_mode",
		Group:          "media",
		PublicCode:     "invalid_image_mode",
		DefaultMessage: "图片格式或颜色模式不受支持，请转换后重新上传。",
		Aliases:        []string{"invalid_image_format", "invalid_image_mode", "unsupported_image_media_type"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "unsupported_image_media_type",
		Group:          "media",
		PublicCode:     "unsupported_image_media_type",
		DefaultMessage: "图片格式或颜色模式不受支持，请转换后重新上传。",
		Aliases:        []string{"invalid_image_format", "invalid_image_mode", "unsupported_image_media_type"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_too_large",
		Group:          "media",
		PublicCode:     "image_too_large",
		DefaultMessage: "图片过大，请降低分辨率或压缩文件后再提交。",
		Aliases:        []string{"image_too_large", "image_file_too_large"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_file_too_large",
		Group:          "media",
		PublicCode:     "image_file_too_large",
		DefaultMessage: "图片过大，请降低分辨率或压缩文件后再提交。",
		Aliases:        []string{"image_too_large", "image_file_too_large"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_too_small",
		Group:          "media",
		PublicCode:     "image_too_small",
		DefaultMessage: "图片尺寸过小，请上传更高分辨率的图片。",
		Aliases:        []string{"image_too_small"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "empty_image_file",
		Group:          "media",
		PublicCode:     "empty_image_file",
		DefaultMessage: "图片文件为空，请重新上传。",
		Aliases:        []string{"empty_image_file"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_image_url",
		Group:          "media",
		PublicCode:     "invalid_image_url",
		DefaultMessage: "无法读取图片链接，请检查链接是否可访问，或直接上传图片。",
		Aliases:        []string{"invalid_image_url", "failed_to_download_image"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "failed_to_download_image",
		Group:          "media",
		PublicCode:     "failed_to_download_image",
		DefaultMessage: "无法读取图片链接，请检查链接是否可访问，或直接上传图片。",
		Aliases:        []string{"invalid_image_url", "failed_to_download_image"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_file_not_found",
		Group:          "media",
		PublicCode:     "image_file_not_found",
		DefaultMessage: "找不到引用的图片，请重新上传。",
		Aliases:        []string{"image_file_not_found"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "audio_too_large",
		Group:          "media",
		PublicCode:     "audio_too_large",
		DefaultMessage: "音频过大，请缩短时长或分段提交。",
		Aliases:        []string{"audio_too_large"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "audio_too_short",
		Group:          "media",
		PublicCode:     "audio_too_short",
		DefaultMessage: "音频过短，请提交较长的有效录音。",
		Aliases:        []string{"audio_too_short"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_audio",
		Group:          "media",
		PublicCode:     "invalid_audio",
		DefaultMessage: "音频数据或格式无效，请重新录制或转换格式。",
		Aliases:        []string{"invalid_audio", "invalid_audio_data", "invalid_audio_format"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_audio_data",
		Group:          "media",
		PublicCode:     "invalid_audio_data",
		DefaultMessage: "音频数据或格式无效，请重新录制或转换格式。",
		Aliases:        []string{"invalid_audio", "invalid_audio_data", "invalid_audio_format"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "invalid_audio_format",
		Group:          "media",
		PublicCode:     "invalid_audio_format",
		DefaultMessage: "音频数据或格式无效，请重新录制或转换格式。",
		Aliases:        []string{"invalid_audio", "invalid_audio_data", "invalid_audio_format"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "missing_audio_sample_rate",
		Group:          "media",
		PublicCode:     "missing_audio_sample_rate",
		DefaultMessage: "音频缺少采样率，请补全采样率后重新提交。",
		Aliases:        []string{"missing_audio_sample_rate"},
		ErrorType:      "invalid_request_error",
	},
	{
		Key:            "image_generation_unavailable",
		Group:          "media",
		PublicCode:     "image_generation_unavailable",
		DefaultMessage: "本次未生成图片，请稍后再试。",
		Aliases:        []string{"image_generation_unavailable"},
		ErrorType:      "server_error",
	},
	{
		Key:            "output_length_limit",
		Group:          "media",
		PublicCode:     "",
		DefaultMessage: "输出已达到长度上限，可缩小请求范围或调整输出上限。",
		Aliases:        []string{"length", "response_incomplete"},
		ErrorType:      "",
	},
	{
		Key:            "output_content_filtered",
		Group:          "media",
		PublicCode:     "",
		DefaultMessage: "部分内容因安全策略未能返回。",
		Aliases:        []string{},
		ErrorType:      "",
	},
	{
		Key:            "response_cancelled",
		Group:          "media",
		PublicCode:     "",
		DefaultMessage: "本次请求已取消。",
		Aliases:        []string{},
		ErrorType:      "",
	},
	{
		Key:            "response_incomplete",
		Group:          "media",
		PublicCode:     "",
		DefaultMessage: "本次响应未完成。",
		Aliases:        []string{},
		ErrorType:      "",
	},
}

type codexPublicErrorIndexes struct {
	byKey  map[string]codexPublicErrorDefinition
	byCode map[string]string
}

var codexPublicErrorIndex = func() codexPublicErrorIndexes {
	index := codexPublicErrorIndexes{
		byKey:  make(map[string]codexPublicErrorDefinition, len(codexPublicErrorCatalog)),
		byCode: make(map[string]string, len(codexPublicErrorCatalog)*2),
	}
	for _, definition := range codexPublicErrorCatalog {
		index.byKey[definition.Key] = definition
	}
	for _, definition := range codexPublicErrorCatalog {
		for _, alias := range definition.Aliases {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if alias == "" {
				continue
			}
			// Canonical keys always win over aliases shared by sibling rows.
			if _, isKey := index.byKey[alias]; isKey {
				continue
			}
			if _, exists := index.byCode[alias]; !exists {
				index.byCode[alias] = definition.Key
			}
		}
	}
	return index
}()

func codexPublicErrorByKey(key string) (codexPublicErrorDefinition, bool) {
	definition, ok := codexPublicErrorIndex.byKey[strings.ToLower(strings.TrimSpace(key))]
	return definition, ok
}

// codexPublicErrorKeyFromCode resolves an upstream or gateway code to the stable
// catalog key. Exact catalog keys take precedence over shared aliases.
func codexPublicErrorKeyFromCode(code string) (string, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return "", false
	}
	if definition, ok := codexPublicErrorByKey(code); ok {
		return definition.Key, true
	}
	key, ok := codexPublicErrorIndex.byCode[code]
	return key, ok
}

// codexPublicErrorFallbackKey implements the C09 status fallback. It is used
// only when no trusted internal classification or recognized code exists.
func codexPublicErrorFallbackKey(status int) string {
	switch status {
	case 400, 422:
		return "request_rejected"
	case 401, 403:
		return "credential_unavailable"
	case 402:
		return "service_capacity_unavailable"
	case 407:
		return "service_connection_failed"
	case 408, 504:
		return "response_timeout"
	case 409:
		return "request_conflict"
	case 413:
		return "request_too_large"
	case 415:
		return "unsupported_media_type"
	case 429:
		return "rate_limit_exceeded"
	case 503:
		return "upstream_unavailable"
	case 529:
		return "model_overloaded"
	case 0:
		return "service_error"
	default:
		return "server_error"
	}
}
