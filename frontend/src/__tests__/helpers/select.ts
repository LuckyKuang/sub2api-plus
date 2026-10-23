import { DOMWrapper, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

// Exercise the rendered menu, including teleported and locally stubbed panels.
export async function openSelect(wrapper: VueWrapper<any> | DOMWrapper<Element>) {
  const trigger = wrapper.element.matches('.select-trigger') ? new DOMWrapper(wrapper.element) : wrapper.get('.select-trigger')
  if (trigger.attributes('aria-expanded') !== 'true') await trigger.trigger('click')
  await nextTick()
  const id = trigger.attributes('aria-controls')
  const localPanel = wrapper.element.parentElement?.querySelector(`[id="${id}"]`)
  const panel = document.getElementById(id) ?? localPanel ?? wrapper.find('[role="listbox"]').element
  if (!panel) throw new Error('Selection menu did not open')
  return new DOMWrapper(panel as HTMLElement)
}

export async function chooseSelect(wrapper: VueWrapper<any> | DOMWrapper<Element>, label: string) {
  const panel = await openSelect(wrapper)
  const option = panel.findAll('[role="option"]').find(item => item.text().trim() === label)
  if (!option) throw new Error(`Selection option not found: ${label}`)
  await option.trigger('click')
  await nextTick()
}
