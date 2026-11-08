import i18n from '@/i18n'

/**
 * Store-level replacement for the Vue `useLocaleRefresh` composable.
 *
 * Stores are module singletons that live outside React's component tree, so they cannot use a
 * hook. They subscribe to i18next's `languageChanged` event instead, which the i18n layer emits
 * whenever `setLocale()` switches language.
 */

/** Refresh translated server data without remounting forms or replaying writes. */
export function onLocaleRefresh(refresh: () => void | Promise<unknown>): void {
  const listener = (): void => {
    void Promise.resolve()
      .then(refresh)
      .catch((error) => {
        console.error('Failed to refresh translated data', error)
      })
  }

  i18n.on('languageChanged', listener)
}
