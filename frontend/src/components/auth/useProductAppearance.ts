import { useEffect } from 'react'

// The root owns the palette so body-portalled controls share the page theme.
// Reference counting also covers overlapping layouts during route transitions.
const owners = new WeakMap<HTMLElement, number>()

export function useProductAppearance(enabled: () => boolean = () => true): void {
  useEffect(() => {
    if (!enabled()) return
    const root = document.documentElement
    owners.set(root, (owners.get(root) ?? 0) + 1)
    root.setAttribute('data-product-ui', '')
    return () => {
      const remaining = (owners.get(root) ?? 1) - 1
      if (remaining > 0) owners.set(root, remaining)
      else {
        owners.delete(root)
        root.removeAttribute('data-product-ui')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
}

export default useProductAppearance
