// TODO(port): local stand-in for `@/constants/platformCatalog` until that module is ported.
// Keeps the exact call surface the utils layer needs (`listPlatforms`, `getPlatformSpec`).
// Values mirror the backend-synced builtin platform catalog; delete this file and
// switch the importers back to `@/constants/platformCatalog` once it lands.

export interface PlatformSpec {
  id: string
  display_name: string
}

const BUILTIN_PLATFORMS: PlatformSpec[] = [
  { id: 'anthropic', display_name: 'Anthropic' },
  { id: 'openai', display_name: 'OpenAI' },
  { id: 'gemini', display_name: 'Gemini' },
  { id: 'antigravity', display_name: 'Antigravity' },
  { id: 'grok', display_name: 'Grok' },
  { id: 'kimi', display_name: 'Kimi' },
  { id: 'zhipu', display_name: 'Zhipu GLM' },
  { id: 'deepseek', display_name: 'DeepSeek' },
  { id: 'minimax', display_name: 'MiniMax' },
  { id: 'stepfun', display_name: 'StepFun' },
  { id: 'opencode_go', display_name: 'OpenCode' },
  { id: 'typesafe', display_name: 'TypeSafe / Jev' },
  { id: 'command_code', display_name: 'Command Code' },
  { id: 'cline', display_name: 'Cline' }
]

/** 全部具体平台，按展示顺序。 */
export function listPlatforms(): PlatformSpec[] {
  return BUILTIN_PLATFORMS
}

export function getPlatformSpec(platform: string | null | undefined): PlatformSpec | undefined {
  if (!platform) return undefined
  return BUILTIN_PLATFORMS.find(spec => spec.id === platform)
}
