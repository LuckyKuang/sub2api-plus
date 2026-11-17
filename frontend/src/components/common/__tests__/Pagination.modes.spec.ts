import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import Pagination from '../Pagination.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }) }))
enableAutoUnmount(afterEach)

type Props = InstanceType<typeof Pagination>['$props']

function mountPagination(props: Partial<Props> = {}) {
  return mount(Pagination, {
    props: { total: 200, page: 5, pageSize: 20, showPageSizeSelector: false, ...props } as Props,
    global: { stubs: { Icon: true } }
  })
}

const pageLabels = (scope: ReturnType<ReturnType<typeof mountPagination>['get']>) =>
  scope.findAll('.pagination-button').map(button => button.text()).filter(Boolean)

// Every pager is numbered: the user rejected bare previous/next pagers, so
// narrow containers keep page numbers with a tighter window instead.
describe('pagination modes', () => {
  it('numbers pages in both the wide and the narrow layout', () => {
    const wrapper = mountPagination()
    expect(pageLabels(wrapper.get('.pagination-desktop'))).toEqual(['1', '...', '3', '4', '5', '6', '7', '...', '10'])
    expect(pageLabels(wrapper.get('.pagination-mobile'))).toEqual(['1', '...', '4', '5', '6', '...', '10'])
    expect(wrapper.text()).not.toContain('pagination.pageOf')
  })

  it('renders without the card shell when embedded', () => {
    const wrapper = mountPagination({ embedded: true })
    expect(wrapper.classes()).toContain('pagination-embedded')
    expect(wrapper.classes()).not.toContain('card')
    expect(mountPagination().classes()).toEqual(expect.arrayContaining(['pagination-card', 'card']))
  })

  it('locks every page control while disabled', async () => {
    const wrapper = mountPagination({ disabled: true })
    const enabled = wrapper.findAll('button').filter(button => !(button.element as HTMLButtonElement).disabled)
    expect(enabled.map(button => button.attributes('aria-current'))).toEqual(['page', 'page'])
    await wrapper.get('.pagination-desktop button[aria-label="pagination.next"]').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })

  it('exposes reached pages plus the next one for cursor lists without a total', async () => {
    const wrapper = mountPagination({ total: null, page: 3, hasMore: true })
    expect(pageLabels(wrapper.get('.pagination-desktop'))).toEqual(['1', '2', '3', '4'])
    expect(wrapper.text()).toContain('pagination.pageOf:{"page":3,"total":4}')
    await wrapper.get('.pagination-desktop button[aria-label="pagination.next"]').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[4]])

    await wrapper.setProps({ page: 4, hasMore: false })
    expect(pageLabels(wrapper.get('.pagination-desktop'))).toEqual(['1', '2', '3', '4'])
    expect(wrapper.get('.pagination-desktop button[aria-label="pagination.next"]').attributes('disabled')).toBeDefined()
  })

  it('lets owners replace the summary text', () => {
    const wrapper = mount(Pagination, {
      props: { total: 30, page: 1, pageSize: 20, showPageSizeSelector: false },
      slots: { info: 'custom summary' },
      global: { stubs: { Icon: true } }
    })
    expect(wrapper.get('.pagination-info').text()).toBe('custom summary')
  })
})
