import { shallowRef } from 'vue'

/** Shared with canvas renderers, which cannot resolve CSS custom properties. */
export const consoleCardAlpha = shallowRef(1)
