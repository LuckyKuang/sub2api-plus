import { identityPresets, type IdentityAccountPolicy, type IdentityPreset } from '@/api/admin/outboundIdentity'

// An API response fixture, deliberately independent of any frontend selector.
export function identityPolicyFixture(): IdentityAccountPolicy[] {
  const compatible: [string, IdentityPreset][] = [
    ['openai:apikey', 'codex'], ['openai:upstream', 'codex'],
    ['anthropic:apikey', 'claude'], ['anthropic:upstream', 'claude'],
    ['gemini:apikey', 'gemini'], ['gemini:upstream', 'gemini'],
    ['grok:apikey', 'grok'], ['grok:upstream', 'grok'],
    ['antigravity:upstream', 'antigravity'], ['typesafe:apikey', 'codex'], ['opencode_go:apikey', 'codex']
  ]
  const fixed: [string, IdentityPreset][] = [
    ['deepseek:oauth', 'deepseek'], ['deepseek:apikey', 'deepseek'],
    ['kimi:oauth', 'kimi'], ['kimi:apikey', 'kimi'],
    ['minimax:oauth', 'minimax'], ['minimax:apikey', 'minimax_apikey'],
    ['zhipu:oauth', 'zcode'], ['zhipu:apikey', 'zcode'],
    ['stepfun:oauth', 'stepfun'], ['stepfun:apikey', 'stepfun'],
    ['anthropic:oauth', 'claude'], ['anthropic:setup-token', 'claude'],
    ['anthropic:bedrock', 'claude'], ['anthropic:service_account', 'claude'], ['gemini:service_account', 'gemini']
  ]
  return [
    ...compatible.map(([key, native_preset]) => ({ key, native_preset, allowed_presets: [...identityPresets], allow_default_mapping: true })),
    ...fixed.map(([key, native_preset]) => ({ key, native_preset, allowed_presets: [native_preset], allow_default_mapping: false }))
  ]
}
