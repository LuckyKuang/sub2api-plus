export default {
  imageTasks: {
    title: '生图任务',
    description: '在一个入口完成普通生图与供应商批量生图。',
    modes: {
      ordinary: { title: '普通生图', hint: '使用 API Key 生成或编辑图片' },
      batch: { title: '批量生图', hint: '多提示词任务，使用供应商批量价格' },
    },
    unavailable: {
      title: '当前没有可用的生图模式',
      hint: '管理员尚未为本站开启任何生图模式。',
      ordinary: '普通生图：{reason}',
      batch: '批量生图：{reason}',
    },
    reasons: {
      ready: '可用',
      disabled: '已关闭',
      not_configured: '对象存储未配置完整',
      no_eligible_key: '没有可用的 API Key',
    },
  },
}
