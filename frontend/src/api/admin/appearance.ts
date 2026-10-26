import { apiClient } from '@/api/client'
import type { ConsoleAppearance, ConsoleAppearanceUpdate } from '@/types/appearance'

export async function getConsoleAppearance(): Promise<ConsoleAppearance> {
  const { data } = await apiClient.get<ConsoleAppearance>('/admin/settings/appearance')
  return data
}

export async function saveConsoleAppearance(update: ConsoleAppearanceUpdate): Promise<ConsoleAppearance> {
  const { data } = await apiClient.put<ConsoleAppearance>('/admin/settings/appearance', update)
  return data
}
