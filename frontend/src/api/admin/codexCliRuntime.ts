import { apiClient } from '../client'

export interface CodexCLIRuntimeStatus {
  target_version: string
  installed_version: string
  path: string
  source: string
  source_verified: boolean
  platform: string
  installed: boolean
  version_match: boolean
  can_install: boolean
  execution_available: boolean
  unavailable_code: string
  message: string
  mode: 'exec_task'
  transparent_proxy_available: boolean
}

export async function getCodexCLIRuntime(): Promise<CodexCLIRuntimeStatus> {
  return (await apiClient.get<CodexCLIRuntimeStatus>('/admin/settings/codex-cli')).data
}

export async function installCodexCLI(): Promise<CodexCLIRuntimeStatus> {
  return (await apiClient.post<CodexCLIRuntimeStatus>(
    '/admin/settings/codex-cli/install',
    undefined,
    { timeout: 310000 }
  )).data
}
