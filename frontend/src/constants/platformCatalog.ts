// TODO(port): placeholder pending constants/ port
// Minimal placeholder exporting only listPlatformIds imported by src/api/admin.
// The real module derives ids from the backend-synced builtin platform catalog.
const BUILTIN_PLATFORM_IDS: string[] = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'stepfun',
  'opencode_go',
  'typesafe',
  'command_code',
  'cline'
]

export function listPlatformIds(): string[] {
  return BUILTIN_PLATFORM_IDS.slice()
}
