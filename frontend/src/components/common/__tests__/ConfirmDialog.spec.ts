import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import ConfirmDialog from '../ConfirmDialog.vue'

const BaseDialogStub = defineComponent({
  emits: ['close'],
  template: '<div><button data-testid="dialog-close" @click="$emit(\'close\')">close</button><slot /><slot name="footer" /></div>',
})

function mountDialog(loading: boolean) {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: { common: { confirm: 'Confirm', cancel: 'Cancel' } } },
  })
  return mount(ConfirmDialog, {
    props: {
      show: true,
      title: 'Confirm action',
      message: 'Proceed?',
      confirmText: 'Confirm',
      cancelText: 'Cancel',
      loading,
    },
    global: {
      plugins: [i18n],
      stubs: { BaseDialog: BaseDialogStub },
    },
  })
}

describe('ConfirmDialog', () => {
  it('blocks confirm, cancel, and dialog-close events while loading', async () => {
    const wrapper = mountDialog(true)
    const buttons = wrapper.findAll('button')

    expect(buttons).toHaveLength(3)
    expect(buttons[1].attributes('disabled')).toBeDefined()
    expect(buttons[2].attributes('disabled')).toBeDefined()
    expect(buttons[2].attributes('aria-busy')).toBe('true')

    await buttons[0].trigger('click')
    await buttons[1].trigger('click')
    await buttons[2].trigger('click')

    expect(wrapper.emitted('cancel')).toBeUndefined()
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it('emits actions normally after loading finishes', async () => {
    const wrapper = mountDialog(true)
    await wrapper.setProps({ loading: false })
    const buttons = wrapper.findAll('button')

    await buttons[0].trigger('click')
    await buttons[1].trigger('click')
    await buttons[2].trigger('click')

    expect(wrapper.emitted('cancel')).toHaveLength(2)
    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })
})
