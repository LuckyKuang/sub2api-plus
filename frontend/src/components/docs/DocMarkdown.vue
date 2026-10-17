<template>
  <div ref="container" class="docs-markdown" v-html="html" @click="handleClick"></div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { renderDocMarkdown, type DocsTocItem } from '@/docs/render'

const props = defineProps<{
  source: string
  variables: Record<string, string>
}>()

const emit = defineEmits<{
  toc: [items: DocsTocItem[]]
}>()

const { t } = useI18n()
const router = useRouter()
const container = ref<HTMLElement | null>(null)
const html = ref('')
let renderVersion = 0

async function render() {
  const version = ++renderVersion
  const rendered = renderDocMarkdown(props.source, props.variables)
  if (version !== renderVersion) return
  html.value = rendered.html
  emit('toc', rendered.toc)
  await nextTick()
  if (version !== renderVersion || !container.value) return
  prepareLinks()
  injectCopyButtons()
}

function prepareLinks() {
  const root = container.value
  if (!root) return
  root.querySelectorAll('a').forEach((anchor) => {
    const href = anchor.getAttribute('href') ?? ''
    if (/^https?:\/\//i.test(href)) {
      anchor.setAttribute('target', '_blank')
      anchor.setAttribute('rel', 'noopener noreferrer')
    }
  })
}

function handleClick(event: MouseEvent) {
  const target = event.target as HTMLElement | null
  const anchor = target?.closest('a') as HTMLAnchorElement | null
  if (!anchor) return
  const href = anchor.getAttribute('href') ?? ''
  if (href.startsWith('/docs')) {
    event.preventDefault()
    void router.push(href)
  }
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Fall through to the legacy path for non-secure contexts.
  }
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', '')
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const copied = document.execCommand('copy')
    document.body.removeChild(textarea)
    return copied
  } catch {
    return false
  }
}

function injectCopyButtons() {
  const root = container.value
  if (!root) return
  root.querySelectorAll('pre').forEach((pre) => {
    if (pre.querySelector('.docs-copy-btn')) return
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'docs-copy-btn'
    button.textContent = t('docs.copy')
    button.addEventListener('click', async () => {
      const code = pre.querySelector('code')?.textContent ?? pre.textContent ?? ''
      const copied = await copyText(code)
      button.textContent = copied ? t('docs.copied') : t('docs.copyFailed')
      window.setTimeout(() => {
        button.textContent = t('docs.copy')
      }, 2000)
    })
    pre.style.position = 'relative'
    pre.appendChild(button)
  })
}

watch(
  () => [props.source, props.variables] as const,
  () => {
    void render()
  },
  { immediate: true, deep: true },
)
</script>

<style scoped>
.docs-markdown {
  color: #374151;
  font-size: 0.9375rem;
  line-height: 1.75;
  overflow-wrap: anywhere;
}

.docs-markdown :deep(h1) {
  margin: 0 0 1rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid #e5e7eb;
  font-size: 1.875rem;
  font-weight: 700;
  line-height: 1.25;
}

.docs-markdown :deep(h2) {
  margin: 2rem 0 0.75rem;
  font-size: 1.25rem;
  font-weight: 650;
  scroll-margin-top: 6rem;
}

.docs-markdown :deep(h3) {
  margin: 1.5rem 0 0.5rem;
  font-size: 1.05rem;
  font-weight: 600;
  scroll-margin-top: 6rem;
}

.docs-markdown :deep(p) {
  margin: 0.75rem 0;
}

.docs-markdown :deep(ul),
.docs-markdown :deep(ol) {
  margin: 0.75rem 0;
  padding-left: 1.5rem;
}

.docs-markdown :deep(ul) {
  list-style: disc;
}

.docs-markdown :deep(ol) {
  list-style: decimal;
}

.docs-markdown :deep(li) {
  margin: 0.25rem 0;
}

.docs-markdown :deep(a) {
  color: #2563eb;
  text-decoration: underline;
  text-underline-offset: 0.15em;
}

.docs-markdown :deep(a:hover) {
  color: #1d4ed8;
}

.docs-markdown :deep(code) {
  border-radius: 0.25rem;
  background: #f3f4f6;
  padding: 0.15rem 0.35rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.85em;
}

.docs-markdown :deep(pre) {
  margin: 1rem 0;
  overflow-x: auto;
  border-radius: 0.75rem;
  background: #111827;
  padding: 1rem;
  color: #f9fafb;
}

.docs-markdown :deep(pre code) {
  background: transparent;
  padding: 0;
  color: inherit;
  font-size: 0.8125rem;
  line-height: 1.7;
}

.docs-markdown :deep(blockquote) {
  margin: 1rem 0;
  border-left: 4px solid #d1d5db;
  padding: 0.25rem 0 0.25rem 1rem;
  color: #4b5563;
}

.docs-markdown :deep(table) {
  display: block;
  width: 100%;
  margin: 1rem 0;
  overflow-x: auto;
  border-collapse: collapse;
  font-size: 0.875rem;
}

.docs-markdown :deep(th),
.docs-markdown :deep(td) {
  border: 1px solid #e5e7eb;
  padding: 0.5rem 0.75rem;
  text-align: left;
  vertical-align: top;
}

.docs-markdown :deep(th) {
  background: #f9fafb;
  font-weight: 600;
}

.docs-markdown :deep(hr) {
  margin: 2rem 0;
  border: 0;
  border-top: 1px solid #e5e7eb;
}

.docs-markdown :deep(.docs-copy-btn) {
  position: absolute;
  top: 0.5rem;
  right: 0.5rem;
  border: 1px solid rgb(255 255 255 / 20%);
  border-radius: 0.375rem;
  background: rgb(255 255 255 / 10%);
  padding: 0.2rem 0.5rem;
  color: #e5e7eb;
  font-size: 0.6875rem;
  cursor: pointer;
  opacity: 0;
  transition: opacity 120ms ease, background-color 120ms ease;
}

.docs-markdown :deep(pre:hover .docs-copy-btn),
.docs-markdown :deep(.docs-copy-btn:focus-visible) {
  opacity: 1;
}

.docs-markdown :deep(.docs-copy-btn:hover) {
  background: rgb(255 255 255 / 20%);
}

.dark .docs-markdown {
  color: #d1d5db;
}

.dark .docs-markdown :deep(h1) {
  border-color: #374151;
  color: #f9fafb;
}

.dark .docs-markdown :deep(h2),
.dark .docs-markdown :deep(h3) {
  color: #f3f4f6;
}

.dark .docs-markdown :deep(a) {
  color: #93c5fd;
}

.dark .docs-markdown :deep(a:hover) {
  color: #bfdbfe;
}

.dark .docs-markdown :deep(code) {
  background: #1f2937;
  color: #f3f4f6;
}

.dark .docs-markdown :deep(blockquote) {
  border-color: #4b5563;
  color: #9ca3af;
}

.dark .docs-markdown :deep(th),
.dark .docs-markdown :deep(td) {
  border-color: #374151;
}

.dark .docs-markdown :deep(th) {
  background: #111827;
}

.dark .docs-markdown :deep(hr) {
  border-color: #374151;
}
</style>
