import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import HourlyUsageDrawer from '../HourlyUsageDrawer.vue'
import UsageHeatmap from '../UsageHeatmap.vue'
import zh from '@/i18n/locales/zh'
import type { HourlyUsageReport } from '@/api/usage'

const api = vi.hoisted(() => ({ getHourlySummary: vi.fn() }))
vi.mock('@/api/usage', () => ({ usageAPI: api }))
vi.mock('@/components/common/BaseDialog.vue', () => ({ default: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /></section>' } }))
const row = { hour: '2026-01-01T01:00:00Z', model: 'gpt-example', billing_type: 0, requests: '1', tokens: '9007199254740993', images: '2', videos: '1', video_seconds: '5', cost: '0.1000000001' }
const fixture: HourlyUsageReport = {
  items: [row, { ...row, model: 'subscription-model', billing_type: 1, cost: '0.2' }],
  trend: [], totals: row, balance_cost: '0.1000000001', subscription_cost: '0.2',
  complete_from: '2025-01-01T00:00:00Z', missing_months: [], backfill_pending: false,
  generated_at: '2026-01-01T01:30:00Z', timezone: 'Asia/Shanghai',
}
let wrapper: ReturnType<typeof mount> | undefined
const global = () => ({ plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })] })
function render() {
  wrapper = mount(HourlyUsageDrawer, { props: { show: true, initialDate: '2026-01-01', filters: { group_id: 8 } }, global: global() })
  return wrapper
}
beforeEach(() => { vi.resetAllMocks(); api.getHourlySummary.mockResolvedValue(structuredClone(fixture)) })
afterEach(() => { wrapper?.unmount() })
describe('retained hourly usage', () => {
  it('loads selected day and filters, sums exactly and keeps quantities and subscriptions distinct', async () => {
    const w = render(); await flushPromises()
    expect(api.getHourlySummary).toHaveBeenCalledTimes(1)
    expect(api.getHourlySummary.mock.calls[0][0]).toMatchObject({ group_id: 8, start_date: '2026-01-01', end_date: '2026-01-01', granularity: 'hour' })
    expect(w.text()).toContain('18,014,398,509,481,986')
    expect(w.text()).toContain('$0.3000')
    await w.get('input[type=checkbox]').setValue(true)
    expect(w.text()).toContain('$0.3000000001')
    expect(w.text()).toContain('4 张')
    expect(w.text()).toContain('10 秒视频')
    expect(w.text()).toContain('统计中')
    expect(w.text()).toContain('订阅消费 $0.2')
    await w.get('select').setValue('gpt-example')
    expect(w.findAll('tbody tr')).toHaveLength(1)
    expect(w.text()).toContain('$0.1000000001')
    expect(w.text()).toContain('9,007,199,254,740,993')
  })
  it('never describes unavailable or backfilling history as zero activity', async () => {
    api.getHourlySummary.mockResolvedValue({ ...fixture, items: [], complete_from: '2026-02-01T00:00:00Z' })
    const w = render(); await flushPromises()
    expect(w.text()).toContain('历史记录不完整')
    expect(w.text()).toContain('不能确认此时段没有消费')
    api.getHourlySummary.mockResolvedValue({ ...fixture, items: [], backfill_pending: true })
    await w.get('.hourly-filters button').trigger('click'); await flushPromises()
    expect(w.text()).toContain('正在汇总')
    expect(w.text()).not.toContain('当日没有符合筛选条件的消费')
  })
  it('discards a stale response after date changes and supports retry', async () => {
    let resolve!: (v: HourlyUsageReport) => void
    api.getHourlySummary.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const w = render(); await flushPromises()
    await w.get('input').setValue('2026-01-02'); await flushPromises()
    resolve({ ...fixture, items: [{ ...row, model: 'STALE' }] }); await flushPromises()
    expect(w.text()).not.toContain('STALE')
    api.getHourlySummary.mockRejectedValueOnce(new Error('offline'))
    await w.get('.hourly-filters button').trigger('click'); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    await w.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(w.findAll('tbody tr')).toHaveLength(2)
  })
  it('opens details separately from metrics and hatches incomplete heatmap dates', async () => {
    const w = mount(UsageHeatmap, { props: { data: [], startDate: '2025-01-01', endDate: '2026-01-01', coverage: { ...fixture, complete_from: '2025-12-31T16:00:00Z' } }, global: global() })
    try {
      await w.get('.heatmap-details').trigger('click')
      expect(w.emitted('details')).toHaveLength(1)
      expect(w.get('[data-date="2025-12-31"]').classes()).toContain('is-incomplete')
      expect(w.get('[data-date="2026-01-01"]').classes()).not.toContain('is-incomplete')
      expect(w.text()).not.toContain(zh.usage.personal.noActivity)
    } finally { w.unmount() }
  })
})
