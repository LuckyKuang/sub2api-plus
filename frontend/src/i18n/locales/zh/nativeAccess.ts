export default {
  "nativeAccess": {
    "copyBase": "复制基础地址", "copyKey": "复制 API 密钥", "copyExample": "复制示例",
    "channelHint": "聚合分组在此绑定一次。选择下方任意供应平台编辑模型价格与映射，价格保留真实供应平台。同一渠道内共享价卡，需要独立价格时请创建独立渠道。",
    "title": "聚合原生接入",
    "hint": "选择默认客户端和原生端点。这把密钥也可以使用各模型列出的其他原生端点。请求不会在不同协议之间转换。",
    "client": "客户端",
    "endpoint": "原生请求端点",
    "baseUrl": "SDK 基础地址",
    "requestUrl": "完整请求地址",
    "models": "支持此端点的模型",
    "noModels": "没有模型支持此客户端与端点组合。",
    "allModels": "查看所有模型的原生端点",
    "custom": "自定义客户端",
    "loadFailed": "无法加载原生端点，请重新选择分组后重试。"
  },
  "longContext": {
    "title": "长上下文阶梯定价",
    "hint": "模型单独覆盖 > 平台默认值 > 模型价格预设 > 0。留空继承；明确填写 0 关闭阶梯，账号开关不能重新开启；改为正数可重新开启。阈值按整次请求的输入（包含缓存读写）计算，不改变缓存策略或上下文窗口。关闭时保留高阶价格，重新开启后复用。",
    "platformDefaults": "平台默认阈值",
    "modelOverrides": "模型阈值与高阶价格覆盖",
    "inherit": "继承已有预设",
    "threshold": "输入阈值（0 关闭）",
    "mode": "高阶定价方式",
    "prices": "自定义价格",
    "multipliers": "自定义倍率",
    "input": "输入",
    "output": "输出",
    "cache_write": "缓存写入（5m）",
    "cache_write_1h": "缓存写入（1h）",
    "cache_read": "缓存读取",
    "priceUnit": "美元 / 百万 Token",
    "factorUnit": "× 基础价格",
    "invalid": "请检查阈值、唯一的精确模型名和高阶价格。自定义定价必须填写输入与输出，缓存价格留空则继承。"
  }
}
