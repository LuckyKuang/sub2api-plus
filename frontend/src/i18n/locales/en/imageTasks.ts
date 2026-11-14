export default {
  imageTasks: {
    title: 'Image Tasks',
    description: 'Generate images and run provider batch jobs from one place.',
    modes: {
      ordinary: { title: 'Standard', hint: 'Generate or edit images with an API key' },
      batch: { title: 'Batch', hint: 'Provider batch pricing with multiple prompts' },
    },
    unavailable: {
      title: 'Image tasks are not available',
      hint: 'An administrator has not enabled any generation mode for this deployment.',
      ordinary: 'Standard mode: {reason}',
      batch: 'Batch mode: {reason}',
    },
    reasons: {
      ready: 'available',
      disabled: 'switched off',
      not_configured: 'object storage is not configured',
      no_eligible_key: 'no eligible API key',
    },
  },
}
