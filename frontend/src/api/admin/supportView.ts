import { apiClient } from '../client'
import type { User } from '@/types'

export type AdminSupportUser = User

export async function getProfile(userId: number): Promise<User> {
  const { data } = await apiClient.get<User>(`/admin/support/users/${userId}/user/profile`)
  return data
}
