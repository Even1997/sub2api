import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MonthlyBillsView from '../MonthlyBillsView.vue'
import bills from '@/i18n/locales/zh/bills'
import type { MonthlyBill } from '@/api/monthlyBills'
import { billMoney, billUsage, previousMonth, shanghaiMonth } from '@/utils/monthlyBill'

const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), generate: vi.fn() }))
vi.mock('@/api/monthlyBills', () => ({ monthlyBillsAPI: api }))
const branding = vi.hoisted(() => ({ siteName: '测试站点', siteLogo: '' }))
vi.mock('@/stores/app', () => ({ useAppStore: () => branding }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' } }))

const model = { model: 'example-model', requests: '2', tokens: '9007199254740993', images: '2', video_seconds: '0', videos: '0', cost: '0.1000000001' }
const fixture: MonthlyBill = {
  month: '2026-09', version: 1, user_id: 1, email: 'demo@example.test', generated_at: '2026-10-27T07:30:00Z',
  start: '2026-08-31T16:00:00Z', end: '2026-09-30T16:00:00Z', partial: false, timezone: 'Asia/Shanghai', cny_per_credit: '1', policy: 'balance-minus-transferred-rebate-v1',
  consumption: '0.1000000001', rebate: '1', net: '-0.8999999999', cny: '-0.8999999999', subscription: '0', models: [model], subscription_models: [],
  days: Array.from({ length: 30 }, (_, i) => ({ date: `2026-09-${String(i + 1).padStart(2, '0')}`, consumption: i === 0 ? '0.1000000001' : '0', rebate: i === 0 ? '1' : '0', net: i === 0 ? '-0.8999999999' : '0', models: i === 0 ? [model] : [], transfers: i === 0 ? [{ id: 7, at: '2026-09-01T00:00:00Z', amount: '1' }] : [] })),
}
let wrapper: ReturnType<typeof mount> | undefined
async function choose(w: NonNullable<typeof wrapper>, selector: string, value: string | number) {
  const select = w.getComponent(selector)
  select.vm.$emit('update:modelValue', value)
  select.vm.$emit('change', value)
  await flushPromises()
}
function render() {
  wrapper = mount(MonthlyBillsView, { global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { bills, common: { cancel: '取消' } } } })] } })
  return wrapper
}
beforeEach(() => { branding.siteName = '测试站点'; vi.resetAllMocks(); api.list.mockResolvedValue([]); api.get.mockResolvedValue(fixture); api.generate.mockResolvedValue(fixture) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('monthly statements', () => {
  it('uses configured site branding in both heading and footer', async () => {
    branding.siteName = '自定义站点名称'
    api.list.mockResolvedValue([{ version: 1, generated_at: fixture.generated_at }])
    const w = render(); await flushPromises()
    expect(w.get('.bill-brand').text()).toBe('自定义站点名称')
    expect(w.get('.bill-footer').text()).toContain('自定义站点名称')
    expect(w.text()).not.toContain('小羊咩咩')
  })

  it('does not generate on entry, month selection or remount; double clicks issue one request', async () => {
    const w = render(); await flushPromises()
    expect(api.generate).not.toHaveBeenCalled()
    await choose(w, '[data-test="bill-month"]', '2026-08')
    expect(api.generate).not.toHaveBeenCalled()
    let resolve!: (b: MonthlyBill) => void
    api.generate.mockImplementation(() => new Promise(r => { resolve = r }))
    await w.get('[data-test="bill-generate"]').trigger('click')
    await w.get('[data-test="bill-generate"]').trigger('click')
    expect(api.generate).toHaveBeenCalledTimes(1)
    expect(api.generate).toHaveBeenCalledWith('2026-08', 0)
    resolve(fixture); await flushPromises()
    expect(w.get('[data-test="bill-amount"]').text()).toBe('¥-0.90')
    expect(w.text()).toContain('本月返佣净流入')
    expect(w.findAll('.bill-models thead th').slice(0, 3).map(n => n.text())).toEqual(['模型', '用量', '消费额度'])
    expect(w.text()).not.toContain('倍率')
    w.unmount(); render(); await flushPromises()
    expect(api.generate).toHaveBeenCalledTimes(1)
  })

  it('reads saved versions and regenerates only after explicit confirmation from latest version', async () => {
    api.list.mockResolvedValue([{ version: 2, generated_at: fixture.generated_at }, { version: 1, generated_at: fixture.generated_at }])
    api.get.mockResolvedValue({ ...fixture, version: 2 })
    api.generate.mockResolvedValue({ ...fixture, version: 3 })
    const w = render(); await flushPromises()
    await choose(w, '[data-test="bill-version"]', 1)
    expect(api.generate).not.toHaveBeenCalled()
    await w.get('[data-test="bill-generate"]').trigger('click'); await flushPromises()
    expect(api.generate).not.toHaveBeenCalled(); expect(w.find('[role="dialog"]').exists()).toBe(true)
    await w.get('[data-test="bill-confirm-regenerate"]').trigger('click'); await flushPromises()
    expect(api.generate).toHaveBeenCalledWith(expect.any(String), 2)
    expect(w.getComponent('[data-test="bill-version"]').props('options')).toHaveLength(3)
    await w.get('.bill-day-button').trigger('click')
    expect(w.get('.bill-day-button').attributes('aria-expanded')).toBe('true')
    await w.get('input[type="checkbox"]').setValue(true)
    expect(w.get('[data-test="bill-amount"]').text()).toBe('¥-0.8999999999')
  })

  it('shows incomplete history errors without fabricating a bill; retries use the same base version', async () => {
    api.generate.mockRejectedValue({ message: '历史数据不完整' })
    const w = render(); await flushPromises()
    await w.get('[data-test="bill-generate"]').trigger('click'); await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('历史数据不完整')
    expect(w.find('[data-test="bill-paper"]').exists()).toBe(false)
    await w.get('[data-test="bill-generate"]').trigger('click'); await flushPromises()
    expect(api.generate.mock.calls.map(c => c[1])).toEqual([0, 0])
  })

  it('offers PDF only and preserves full daily records for printing', async () => {
    api.list.mockResolvedValue([{ version: 1, generated_at: fixture.generated_at }])
    const print = vi.spyOn(window, 'print').mockImplementation(() => {})
    const w = render(); await flushPromises()
    expect(w.text()).not.toContain('CSV')
    expect(w.find('[data-test="bill-export"]').exists()).toBe(false)
    expect(w.findAll('.bill-day-button')).toHaveLength(30)
    await w.get('[data-test="bill-print"]').trigger('click')
    expect(print).toHaveBeenCalledTimes(1)
    expect(document.body.classList.contains('printing-bill')).toBe(true)
    window.dispatchEvent(new Event('afterprint'))
    expect(document.body.classList.contains('printing-bill')).toBe(false)
    expect(billUsage(model, k => k)).toContain('9,007,199,254,740,993')
    expect(api.generate).not.toHaveBeenCalled()
    print.mockRestore()
  })

  it('rounds display only and uses Shanghai calendar boundaries', () => {
    expect(billMoney('9007199254740993.005')).toBe('9,007,199,254,740,993.01')
    expect(billMoney('-0.0000000001')).toBe('0.00')
    expect(billMoney('-0.005')).toBe('-0.01')
    expect(billMoney('0.1000000001', true)).toBe('0.1000000001')
    expect(shanghaiMonth(new Date('2026-09-30T16:00:00Z'))).toBe('2026-10')
    expect(previousMonth('2026-01')).toBe('2025-12')
  })
})
