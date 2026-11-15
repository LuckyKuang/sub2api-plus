export default {
  imageTasks: {
    title: '生图任务',
    description: '在一个入口完成普通生图与供应商批量生图。',
    readOnly: '暂不可新建：{reason}。已有任务仍可管理。',
    requestedUnavailable: '{mode}暂不可用：{reason}。如有其他可用模式，将显示该模式。',
    ownership: {
      ordinary: '普通生图历史归属当前用户，包含已删除 API Key 创建的任务。',
      batch: '批量历史仍需使用提交任务的 API Key 管理，供应商结果可能过期或不可用。',
    },
    modes: {
      ordinary: { title: '普通生图', hint: '使用 API Key 生成或编辑图片' },
      batch: { title: '批量生图', hint: '多提示词任务，使用供应商批量价格' },
    },
    unavailable: {
      title: '当前没有可用的生图模式',
      hint: '当前账户暂无可展示的模式，具体原因如下。',
      ordinary: '普通生图：{reason}',
      batch: '批量生图：{reason}',
    },
    reasons: {
      ready: '可用',
      disabled: '已关闭',
      not_configured: '对象存储未配置完整',
      no_eligible_key: '没有可用的 API Key',
      worker_disabled: '后台处理已暂停',
      no_channel: '没有可用渠道',
      dependency_unavailable: '依赖服务暂不可用',
    },
  },
}
