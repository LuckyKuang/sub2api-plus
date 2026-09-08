import { ref } from 'vue'
import { useMutationObserver } from '@vueuse/core'

/** Keep canvas charts in sync with the same root class used by CSS themes. */
export function useDocumentDarkMode() {
  const root = typeof document === 'undefined' ? undefined : document.documentElement
  const isDark = ref(root?.classList.contains('dark') ?? false)
  useMutationObserver(root, () => {
    isDark.value = root?.classList.contains('dark') ?? false
  }, { attributes: true, attributeFilter: ['class'] })
  return isDark
}
