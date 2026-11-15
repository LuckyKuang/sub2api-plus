export default {
  imageTasks: {
    title: 'Image Tasks',
    description: 'Generate images and run provider batch jobs from one place.',
    readOnly: 'New submissions are unavailable: {reason}. Existing tasks remain manageable.',
    requestedUnavailable: '{mode} is unavailable: {reason}. Showing an available mode when possible.',
    ownership: {
      ordinary: 'Standard history belongs to your user account, including tasks from deleted API keys.',
      batch: 'Batch history requires the submitting API key. Provider results may expire or become unavailable.',
    },
    modes: {
      ordinary: { title: 'Standard', hint: 'Generate or edit images with an API key' },
      batch: { title: 'Batch', hint: 'Provider batch pricing with multiple prompts' },
    },
    unavailable: {
      title: 'Image tasks are not available',
      hint: 'No mode is currently available for this account. See the reasons below.',
      ordinary: 'Standard mode: {reason}',
      batch: 'Batch mode: {reason}',
    },
    reasons: {
      ready: 'available',
      disabled: 'switched off',
      not_configured: 'object storage is not configured',
      no_eligible_key: 'no eligible API key',
      worker_disabled: 'background processing is paused',
      no_channel: 'no available channel',
      dependency_unavailable: 'a required service is temporarily unavailable',
    },
  },
}
