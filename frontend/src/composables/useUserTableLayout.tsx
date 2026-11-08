import { createContext, useContext, type ReactNode } from 'react'

/**
 * User layouts keep every selected field visible without horizontal scrolling.
 * Provided by the app shell for user-appearance routes, like the Vue
 * USER_TABLE_LAYOUT_KEY injection it replaces.
 */
const UserTableLayoutContext = createContext(false)

export function UserTableLayoutProvider({
  value,
  children
}: {
  value: boolean
  children: ReactNode
}) {
  return <UserTableLayoutContext.Provider value={value}>{children}</UserTableLayoutContext.Provider>
}

export function useUserTableLayout(): boolean {
  return useContext(UserTableLayoutContext)
}
