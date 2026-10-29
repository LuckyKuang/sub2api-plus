import type { ComputedRef, InjectionKey } from 'vue'

/** User layouts keep every selected field visible without horizontal scrolling. */
export const USER_TABLE_LAYOUT_KEY: InjectionKey<ComputedRef<boolean>> = Symbol('user-table-layout')
