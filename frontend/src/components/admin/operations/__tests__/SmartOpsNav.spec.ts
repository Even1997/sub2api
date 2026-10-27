import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SmartOpsNav from '../SmartOpsNav.vue'

const route = vi.hoisted(() => ({ path: '/admin/priority-scheduling' }))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({
  useRoute: () => route,
  RouterLink: {
    props: ['to'],
    template: '<a :href="to"><slot :is-active="to === route.path" /></a>',
    setup: () => ({ route }),
  },
}))

describe('SmartOpsNav', () => {
  it('keeps existing operations links when priority scheduling is added', () => {
    const wrapper = mount(SmartOpsNav)
    expect(wrapper.findAll('a').map((link) => link.attributes('href'))).toEqual([
      '/admin/auto-config',
      '/admin/priority-scheduling',
      '/admin/channel-operations',
      '/admin/account-quality',
      '/admin/controlled-experiments',
      '/admin/account-ops',
      '/admin/token-guard',
      '/admin/token-guard-v2',
      '/admin/pelican-tests',
      '/admin/group-cache-tests',
      '/admin/gateway-borrow',
    ])
  })
})
