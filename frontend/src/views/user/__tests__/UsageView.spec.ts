import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UsageView from '../UsageView.vue'
import TrafficPanel from '@/components/usage/TrafficPanel.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import UsageHeatmap from '@/components/user/UsageHeatmap.vue'

const {
  query,
  getStats,
  getDashboardModels,
  getDashboardSnapshotV2,
  getHourlySummary,
  listMyErrorRequests,
  list,
  getAvailable,
  showError,
  showWarning,
  showSuccess,
  showInfo,
} = vi.hoisted(() => ({
  query: vi.fn(),
  getStats: vi.fn(),
  getDashboardModels: vi.fn(),
  getDashboardSnapshotV2: vi.fn(),
  getHourlySummary: vi.fn(),
  listMyErrorRequests: vi.fn(),
  list: vi.fn(),
  getAvailable: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
}))

const messages: Record<string, string> = {
  'usage.personal.requestLogs': 'Request Logs',
  'usage.personal.totalCost': 'Standard Cost',
  'admin.dashboard.timeRange': 'Time range',
  'admin.dashboard.granularity': 'Granularity',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
  'admin.users.columnSettings': 'Columns',
  'admin.usage.group': 'Group',
  'admin.usage.billingType': 'Billing type',
  'admin.usage.billingMode': 'Billing mode',
  'admin.usage.allTypes': 'All types',
  'admin.usage.allBillingTypes': 'All billing types',
  'admin.usage.billingTypeBalance': 'Balance',
  'admin.usage.billingTypeSubscription': 'Subscription',
  'admin.usage.allBillingModes': 'All billing modes',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per request',
  'admin.usage.billingModeImage': 'Image',
  'admin.usage.allGroups': 'All groups',
  'admin.usage.allModels': 'All models',
  'usage.allApiKeys': 'All API Keys',
  'usage.errors.allKeys': 'All API Keys',
  'usage.tabs.usage': 'Usage records',
  'usage.tabs.errors': 'Error records',
  'usage.apiKeyFilter': 'API Key',
  'usage.totalRequests': 'Total Requests',
  'usage.model': 'Model',
  'usage.type': 'Type',
  'usage.ws': 'WS',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'usage.compactionFilter': 'Request Kind',
  'usage.allCompactionTypes': 'All Requests',
  'usage.compactionOnly': 'Compaction Only',
  'usage.exporting': 'Exporting',
  'usage.exportCsv': 'Export CSV',
  'usage.failedToLoad': 'Failed to load',
  'usage.noDataToExport': 'No data',
  'usage.preparingExport': 'Preparing export',
  'usage.exportSuccess': 'Export success',
  'usage.exportFailed': 'Export failed',
  'common.refresh': 'Refresh',
  'common.all': 'All',
  'common.reset': 'Reset',
}

vi.mock('@/api', () => ({
  usageAPI: {
    query,
    getStats,
    getDashboardModels,
    getDashboardSnapshotV2,
    getHourlySummary,
    listMyErrorRequests,
  },
  keysAPI: {
    list,
  },
  userGroupsAPI: {
    getAvailable,
  },
}))

const appStoreState = vi.hoisted(() => ({
  cachedPublicSettings: { allow_user_view_error_requests: true } as Record<string, unknown>,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError, showWarning, showSuccess, showInfo,
    get cachedPublicSettings() {
      return appStoreState.cachedPublicSettings
    },
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: { date?: string }) => (messages[key] ?? key) + (params?.date ? ' ' + params.date : ''),
    }),
  }
})

const simpleStub = { template: '<div><slot /></div>' }
const chartStub = { template: '<div />' }

const usageLog = {
  id: 1,
  request_id: 'req-user-export',
  actual_cost: 0.092883,
  total_cost: 0.092883,
  rate_multiplier: 1,
  service_tier: 'priority',
  input_cost: 0.020285,
  output_cost: 0.00303,
  cache_creation_cost: 0.000001,
  cache_read_cost: 0.069568,
  input_tokens: 4057,
  output_tokens: 101,
  cache_creation_tokens: 4,
  cache_read_tokens: 278272,
  cache_creation_5m_tokens: 0,
  cache_creation_1h_tokens: 0,
  image_count: 0,
  image_size: null,
  first_token_ms: 12,
  duration_ms: 345,
  created_at: '2026-03-08T00:00:00Z',
  model: 'gpt-5.4',
  reasoning_effort: null,
  ip_address: '203.0.113.10',
  api_key: { name: 'demo-key' },
  billing_mode: 'token',
  request_type: 'sync',
  stream: false,
  native_compaction_v2: false,
}

function mountUsageView() {
  return mount(UsageView, {
    global: {
      stubs: {
        AppLayout: simpleStub,
        Pagination: true,
        Select: true,
        DateRangePicker: true,
        Icon: true,
        UsageStatsCards: chartStub,
        UsageTable: { ...chartStub, props: ['columns'] },
        UsageHeatmap: true,
        UsageTrendChart: { ...chartStub, name: 'UsageTrendChart', props: ['data', 'startDate', 'endDate', 'granularity', 'loading', 'error'] },
        PlatformIcon: true,
        UserErrorRequestsTable: chartStub,
        TrafficPanel: true,
        ModelDistributionChart: chartStub,
        GroupDistributionChart: chartStub,
        EndpointDistributionChart: chartStub,
        TokenUsageTrend: chartStub,
      },
    },
  })
}

describe('user UsageView', () => {
  beforeEach(() => {
    localStorage.removeItem('user-usage-hidden-columns')
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true }
    query.mockReset()
    getStats.mockReset()
    getDashboardModels.mockReset()
    getDashboardSnapshotV2.mockReset()
    getHourlySummary.mockReset()
    listMyErrorRequests.mockReset()
    list.mockReset()
    getAvailable.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()

    query.mockResolvedValue({ items: [usageLog], total: 1, pages: 1 })
    getStats.mockResolvedValue({
      total_requests: 1,
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_tokens: 0,
      total_tokens: 30,
      total_cost: 0.1,
      total_actual_cost: 0.08,
      average_duration_ms: 12,
      endpoints: [],
      upstream_endpoints: [],
      endpoint_paths: [],
    })
    getDashboardModels.mockResolvedValue({
      models: [{ model: 'gpt-5.4', requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 0.1, actual_cost: 0.08 }],
      start_date: '2026-03-08',
      end_date: '2026-03-08',
    })
    getDashboardSnapshotV2.mockResolvedValue({
      generated_at: '2026-03-08T00:00:00Z',
      start_date: '2026-03-08',
      end_date: '2026-03-08',
      granularity: 'hour',
      trend: [],
      groups: [],
    })
    getHourlySummary.mockResolvedValue({ trend: [], complete_from: '2025-01-01T00:00:00Z', missing_months: [], backfill_pending: false })
    listMyErrorRequests.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    list.mockResolvedValue({ items: [{ id: 1, name: 'demo-key' }], total: 1, page: 1, page_size: 100, pages: 1 })
    getAvailable.mockResolvedValue([{ id: 1, name: 'default' }])
  })

  it('passes the same own API key choices to traffic and usage details', async () => {
    const wrapper = mountUsageView(); await flushPromises()
    const usageOptions = wrapper.findAllComponents(Select).find(select =>
      (select.props('options') as SelectOption[]).some(option => option.label === 'demo-key')
    )!.props('options')
    await wrapper.findAll('button').find(button => button.text() === 'traffic.title')!.trigger('click')
    expect(wrapper.getComponent(TrafficPanel).props('apiKeyOptions')).toEqual(usageOptions)
    wrapper.unmount()
  })

  it('loads logs, stats and heatmap without requesting hidden analysis data on first render', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    expect(wrapper.findComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)).toEqual([
      'created_at', 'api_key', 'group', 'model', 'reasoning_effort', 'endpoint', 'stream', 'billing_mode',
      'input_tokens', 'output_tokens', 'cache_read_tokens', 'tokens', 'cost', 'latency',
    ])
    expect(query).toHaveBeenCalled()
    expect(getStats).toHaveBeenCalled()
    expect(getDashboardModels).toHaveBeenCalled()
    expect(getDashboardSnapshotV2).not.toHaveBeenCalled()
    expect(list).toHaveBeenCalledTimes(1)
    expect(list).toHaveBeenCalledWith(1, 100)
    expect(getAvailable).toHaveBeenCalled()
    expect(getHourlySummary).toHaveBeenCalledWith(expect.objectContaining({ granularity: 'day' }))
    expect(wrapper.findComponent(UsageHeatmap).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'UsageTrendChart' }).exists()).toBe(false)
    wrapper.unmount()
  })

  it('refreshes before filters while preserving the current query and preventing duplicate clicks', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.filters.api_key_id = 1
    vm.filters.model = 'gpt-5.4'
    vm.handleSort('model', 'asc')
    vm.handlePageChange(2)
    await flushPromises()
    const previousParams = { ...query.mock.calls.at(-1)![0] }
    query.mockClear()
    let resolveLogs!: (value: unknown) => void
    query.mockReturnValueOnce(new Promise((resolve) => { resolveLogs = resolve }))
    const refresh = wrapper.find('.usage-record-actions button')
    expect(refresh.attributes('aria-label')).toBe('Refresh')
    expect(refresh.attributes('title')).toBe('Refresh')
    expect(refresh.element.nextElementSibling?.classList.contains('usage-filter-button')).toBe(true)
    await refresh.trigger('click')
    await flushPromises()
    expect(query).toHaveBeenCalledWith(previousParams, expect.anything())
    expect(previousParams).toMatchObject({ api_key_id: 1, model: 'gpt-5.4', page: 2, sort_by: 'model', sort_order: 'asc' })
    expect(refresh.attributes('disabled')).toBeDefined()
    expect(refresh.find('icon-stub').classes()).toContain('animate-spin')
    await refresh.trigger('click')
    expect(query).toHaveBeenCalledTimes(1)
    resolveLogs({ items: [usageLog], total: 1, pages: 1 })
    await flushPromises()
    expect(refresh.attributes('disabled')).toBeUndefined()
    expect(refresh.find('icon-stub').classes()).not.toContain('animate-spin')
    wrapper.unmount()
  })

  it('keeps the annual heatmap independent from date selection and shows separate hourly bars for a single day', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    ;(wrapper.vm as any).filters.api_key_id = 1
    const heatmap = wrapper.findComponent(UsageHeatmap)
    const annualRange = { startDate: heatmap.props('startDate'), endDate: heatmap.props('endDate') }
    getHourlySummary.mockClear()
    expect(wrapper.find('[data-testid="usage-all-dates"]').exists()).toBe(false)
    expect(wrapper.findComponent(UsageHeatmap).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'UsageTrendChart' }).exists()).toBe(false)
    wrapper.findComponent(DateRangePicker).vm.$emit('change', { startDate: '2026-10-13', endDate: '2026-10-13', preset: 'today' })
    await flushPromises()
    expect(heatmap.props()).toMatchObject(annualRange)
    expect(getHourlySummary).not.toHaveBeenCalled()
    expect(wrapper.findComponent({ name: 'UsageTrendChart' }).props('granularity')).toBe('hour')
    expect(getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ granularity: 'hour', start_date: '2026-10-13', end_date: '2026-10-13', api_key_id: 1 }))

    wrapper.findComponent(DateRangePicker).vm.$emit('change', { startDate: '2026-10-01', endDate: '2026-10-13', preset: null })
    await flushPromises()
    expect(heatmap.props()).toMatchObject(annualRange)
    expect(getHourlySummary).not.toHaveBeenCalled()
    expect(wrapper.findComponent({ name: 'UsageTrendChart' }).exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps time first and combined response timing last, and only hides IP, user agent and TPS by default', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    await wrapper.find('[data-testid="usage-column-settings"]').trigger('click')
    expect(wrapper.find('[data-testid="usage-column-toggle-latency"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-testid="usage-column-toggle-first_token_ms"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="usage-column-toggle-duration_ms"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="usage-column-toggle-ip_address"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="usage-column-toggle-user_agent"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="usage-column-toggle-tps"]').attributes('aria-checked')).toBe('false')
    await wrapper.find('[data-testid="usage-column-toggle-user_agent"]').trigger('click')
    expect(wrapper.findComponent(UsageTable).props('columns').at(-1).key).toBe('latency')
    expect(wrapper.find('[data-testid="usage-column-toggle-created_at"]').exists()).toBe(false)
    for (const key of ['reasoning_effort', 'endpoint', 'stream', 'billing_mode', 'tokens']) {
      expect(wrapper.find(`[data-testid="usage-column-toggle-${key}"]`).attributes('aria-checked')).toBe('true')
    }
    wrapper.unmount()
  })

  it('filters by requested model vendor independently of duplicate group platforms', async () => {
    getAvailable.mockResolvedValue([
      { id: 1, name: 'OpenAI relay', platform: 'openai' },
      { id: 2, name: 'Second relay', platform: 'openai' },
      { id: 3, name: 'Mixed relay', platform: 'composite' },
    ])
    const wrapper = mountUsageView()
    await flushPromises()
    expect(wrapper.findAll('.usage-group-tabs button')).toHaveLength(5)
    expect(wrapper.findAll('.usage-group-tabs button[aria-label]').map(button => button.attributes('aria-label'))).toEqual(['OpenAI', 'Claude', 'Grok', 'Gemini'])
    ;(wrapper.vm as any).filters.group_id = 3
    ;(wrapper.vm as any).filters.model = 'gpt-5.4'
    await wrapper.find('.usage-group-tabs button[aria-label="Claude"]').trigger('click')
    await flushPromises()
    const expected = { model_provider: 'anthropic', group_id: 3, model: undefined }
    expect(query).toHaveBeenLastCalledWith(expect.objectContaining(expected), expect.anything())
    for (const endpoint of [getStats, getDashboardModels, getHourlySummary]) {
      expect(endpoint).toHaveBeenLastCalledWith(expect.objectContaining(expected))
    }
    expect(wrapper.find('.usage-group-tabs button[aria-label="Claude"]').attributes('aria-pressed')).toBe('true')
    await wrapper.find('.usage-group-tabs button').trigger('click')
    await flushPromises()
    expect(getStats).toHaveBeenLastCalledWith(expect.objectContaining({ model_provider: undefined, group_id: 3 }))
    wrapper.unmount()
  })

  it('queries all dates without a default week and keeps the heatmap independent', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    const picker = wrapper.findComponent(DateRangePicker)
    expect(picker.props('allowAll')).toBe(true)
    getHourlySummary.mockClear()
    picker.vm.$emit('change', { startDate: '', endDate: '', preset: 'all' })
    await flushPromises()
    const expected = { period: 'all', start_date: undefined, end_date: undefined }
    expect(query).toHaveBeenLastCalledWith(expect.objectContaining(expected), expect.anything())
    for (const endpoint of [getStats, getDashboardModels]) {
      expect(endpoint).toHaveBeenLastCalledWith(expect.objectContaining(expected))
    }
    expect(wrapper.findComponent({ name: 'UsageTrendChart' }).exists()).toBe(false)
    expect(wrapper.findAll('.usage-stat')[0].attributes('title')).toContain('All')
    expect(getHourlySummary).not.toHaveBeenCalled()
    await wrapper.find('.usage-group-tabs button[aria-label="Grok"]').trigger('click')
    await flushPromises()
    expect(getHourlySummary).toHaveBeenLastCalledWith(expect.objectContaining({
      period: undefined, model_provider: 'grok',
      start_date: wrapper.findComponent(UsageHeatmap).props('startDate'),
      end_date: wrapper.findComponent(UsageHeatmap).props('endDate'),
    }))
    wrapper.unmount()
  })

  it('discloses missing historical spending without replacing actual billed amounts', async () => {
    getStats.mockResolvedValue({
      total_requests: 12, total_input_tokens: 100, total_output_tokens: 50,
      total_cache_tokens: 0, total_tokens: 150, total_cost: 100, total_actual_cost: 12.34,
      average_duration_ms: 100, history_incomplete: true,
      records_available_from: '2026-07-15T08:58:02Z',
    })
    const wrapper = mountUsageView()
    await flushPromises()
    expect(wrapper.findAll('.usage-stat-value')[0].text()).toBe('$12.34')
    expect(wrapper.find('.usage-history-notice').text()).toContain('2026-07-15')
    expect(wrapper.find('.usage-history-notice').text()).toContain('usage.personal.retainedHistory')
    getStats.mockResolvedValue({ total_requests: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, history_incomplete: false })
    wrapper.findComponent(DateRangePicker).vm.$emit('change', { startDate: '2026-10-01', endDate: '2026-10-13', preset: null })
    await flushPromises()
    expect(wrapper.find('.usage-history-notice').exists()).toBe(false)
    wrapper.unmount()
  })

  it('preserves saved choices including the combined timing column', async () => {
    localStorage.setItem('user-usage-hidden-columns', JSON.stringify(['model', 'ip_address', 'user_agent', 'latency']))
    const wrapper = mountUsageView()
    await flushPromises()
    const keys = wrapper.findComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)
    expect(keys).not.toContain('latency')
    expect(keys).not.toContain('duration_ms')
    expect(keys).not.toContain('first_token_ms')
    expect(keys).toContain('reasoning_effort')
    expect(keys).not.toContain('model')
    wrapper.unmount()
    localStorage.removeItem('user-usage-hidden-columns')
  })

  it('shows combined timing with saved preferences for the removed standalone columns', async () => {
    localStorage.setItem('user-usage-hidden-columns', JSON.stringify(['model', 'first_token_ms', 'duration_ms', 'tps']))
    const wrapper = mountUsageView()
    await flushPromises()
    const keys = wrapper.findComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)
    expect(keys).toContain('latency')
    expect(keys).not.toContain('model')
    expect(keys).not.toContain('first_token_ms')
    expect(keys).not.toContain('duration_ms')
    await wrapper.find('[data-testid="usage-column-settings"]').trigger('click')
    await wrapper.find('[data-testid="usage-column-toggle-latency"]').trigger('click')
    expect(wrapper.findComponent(UsageTable).props('columns').map((column: { key: string }) => column.key)).not.toContain('latency')
    expect(JSON.parse(localStorage.getItem('user-usage-hidden-columns')!)).toContain('latency')
    wrapper.unmount()
  })

  it('updates all summary cards for the selected dates and ignores older statistics', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    getHourlySummary.mockClear()
    let resolveOld!: (value: unknown) => void
    getStats.mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve }))
    const picker = wrapper.findComponent(DateRangePicker)
    picker.vm.$emit('change', { startDate: '2026-10-01', endDate: '2026-10-05', preset: null })
    await flushPromises()
    expect(wrapper.find('.usage-summary').attributes('aria-busy')).toBe('true')
    expect(wrapper.findAll('.usage-stat-value').map((card) => card.text())).toEqual(['$0.08', '$0.10', '1', '30'])

    const latest = {
      total_requests: 12, total_input_tokens: 100, total_output_tokens: 50,
      total_cache_read_tokens: 75, total_cache_creation_tokens: 25,
      total_tokens: 250, total_cost: 0.3, total_actual_cost: 0.24, average_duration_ms: 100,
    }
    getStats.mockResolvedValueOnce(latest)
    picker.vm.$emit('change', { startDate: '2026-10-13', endDate: '2026-10-13', preset: 'today' })
    await flushPromises()
    expect(getStats).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-13', end_date: '2026-10-13' }))
    expect(query).toHaveBeenLastCalledWith(expect.objectContaining({ start_date: '2026-10-13', end_date: '2026-10-13', page: 1 }), expect.anything())
    expect(wrapper.findAll('.usage-stat-value').map((card) => card.text())).toEqual(['$0.24', '$0.30', '12', '250'])
    expect(wrapper.findAll('.usage-stat')[1].text()).toContain('Standard Cost')
    expect(wrapper.findAll('.usage-stat')[2].text()).toContain('Total Requests')
    expect(wrapper.findAll('.usage-stat')[2].attributes('title')).toContain('2026-10-13 ~ 2026-10-13')
    expect(getHourlySummary).not.toHaveBeenCalled()
    resolveOld({ ...latest, total_requests: 999 })
    await flushPromises()
    expect(wrapper.findAll('.usage-stat-value')[2].text()).toBe('12')
    wrapper.unmount()
  })

  it('clears the previous date statistics if the new range fails to load', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    getStats.mockRejectedValueOnce(new Error('offline'))
    wrapper.findComponent(DateRangePicker).vm.$emit('change', { startDate: '2026-10-13', endDate: '2026-10-13', preset: null })
    await flushPromises()
    expect(wrapper.findAll('.usage-stat-value').map((card) => card.text())).toEqual(['-', '-', '-', '-'])
    expect(showError).toHaveBeenCalledWith('Failed to load')
    wrapper.unmount()
  })

  it('keeps large totals compact with exact values available on hover', async () => {
    getStats.mockResolvedValueOnce({
      total_requests: 274_768_999, total_input_tokens: 100, total_output_tokens: 50,
      total_cache_read_tokens: 75, total_cache_creation_tokens: 25,
      total_tokens: 250, total_cost: 12_345_678.9, total_actual_cost: 12_345_678.9, average_duration_ms: 100,
    })
    const wrapper = mountUsageView()
    await flushPromises()
    const cards = wrapper.findAll('.usage-stat')
    expect(cards[0].find('.usage-stat-value').text()).toBe('$12.3M')
    expect(cards[0].attributes('title')).toContain('$12345678.90')
    expect(cards[1].find('.usage-stat-value').text()).toBe('$12.3M')
    expect(cards[1].attributes('title')).toContain('$12345678.90')
    expect(cards[2].find('.usage-stat-value').text()).toBe('274.8M')
    expect(cards[2].attributes('title')).toContain((274_768_999).toLocaleString())
    wrapper.unmount()
  })

  it('shows spending, original cost, requests and tokens without cache hit rate or timing cards', async () => {
    getStats.mockResolvedValueOnce({
      total_requests: 1, total_input_tokens: 100, total_output_tokens: 9000,
      total_cache_read_tokens: 800, total_cache_creation_tokens: 100,
      total_cache_tokens: 900, total_tokens: 10000, total_cost: 1, total_actual_cost: 0.8,
      average_duration_ms: 100,
    })
    const wrapper = mountUsageView()
    await flushPromises()
    expect(wrapper.findAll('.usage-stat')).toHaveLength(4)
    expect(wrapper.findAll('.usage-stat-value').map(card => card.text())).toEqual(['$0.80', '$1.00', '1', '10.0K'])
    expect(wrapper.find('.usage-summary').text()).not.toMatch(/Cache Hit|首字|TPS|80.0%/)
    expect(wrapper.findAll('.usage-stat')[3].find('icon-stub').exists()).toBe(true)
    wrapper.unmount()
  })

  it('selects a heatmap day into hourly bars without losing other filters', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    ;(wrapper.vm as any).filters.api_key_id = 1
    query.mockClear()
    getStats.mockClear()
    getHourlySummary.mockClear()
    wrapper.findComponent(UsageHeatmap).vm.$emit('select', '2026-03-08')
    await flushPromises()
    expect(query).toHaveBeenCalledWith(expect.objectContaining({ start_date: '2026-03-08', end_date: '2026-03-08', api_key_id: 1, page: 1 }), expect.anything())
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ start_date: '2026-03-08', end_date: '2026-03-08', api_key_id: 1 }))
    expect(getHourlySummary).not.toHaveBeenCalled()
    expect(wrapper.findComponent(UsageHeatmap).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'UsageTrendChart' }).props()).toMatchObject({ startDate: '2026-03-08', endDate: '2026-03-08', granularity: 'hour' })
    wrapper.unmount()
  })

  it('uses Monday-aligned periods and never navigates past today', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    const heatmap = wrapper.findComponent(UsageHeatmap)
    const firstStart = heatmap.props('startDate')
    expect(new Date(`${firstStart}T00:00:00Z`).getUTCDay()).toBe(1)
    heatmap.vm.$emit('navigate', -1)
    await flushPromises()
    expect(heatmap.props('endDate')).toBe(new Date(new Date(`${firstStart}T00:00:00Z`).getTime() - 86400000).toISOString().slice(0, 10))
    expect(heatmap.props('canGoNext')).toBe(true)
    heatmap.vm.$emit('navigate', 1)
    await flushPromises()
    expect(heatmap.props('startDate')).toBe(firstStart)
    expect(heatmap.props('canGoNext')).toBe(false)
    wrapper.unmount()
  })

  it('ignores a stale heatmap response after filters change', async () => {
    let resolveFirst!: (value: { trend: unknown[] }) => void
    getHourlySummary.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
    const wrapper = mountUsageView()
    await flushPromises()
    const latest = [{ date: '2026-03-08', requests: 2 }]
    getHourlySummary.mockResolvedValueOnce({ trend: latest, complete_from: '2025-01-01T00:00:00Z', missing_months: [], backfill_pending: false })
    ;(wrapper.vm as any).filters.model = 'gpt-5.4'
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()
    resolveFirst({ trend: [{ date: '2026-03-07', requests: 999 }] })
    await flushPromises()
    expect(wrapper.findComponent(UsageHeatmap).props('data')).toEqual(latest)
    wrapper.unmount()
  })

  it('exposes a retry state on heatmap failure and clears it after retrying', async () => {
    getHourlySummary.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountUsageView()
    await flushPromises()
    expect(wrapper.findComponent(UsageHeatmap).props('error')).toBe(true)
    wrapper.findComponent(UsageHeatmap).vm.$emit('retry')
    await flushPromises()
    expect(wrapper.findComponent(UsageHeatmap).props('error')).toBe(false)
    wrapper.unmount()
  })

  it('does not mount analysis charts until their tab is selected', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ModelDistributionChart' }).exists()).toBe(false)
    await wrapper.find('[role="tab"][id="usage-tab-models"]').trigger('click')
    expect(wrapper.findComponent(UsageTable).exists()).toBe(false)
    expect(wrapper.find('#usage-view-panel').attributes('aria-labelledby')).toBe('usage-tab-models')
    await wrapper.find('#usage-tab-usage').trigger('click')
    expect(wrapper.findComponent(UsageTable).exists()).toBe(true)
    wrapper.unmount()
  })

  it('loads chart data on demand, reuses it between tabs, and invalidates it when filters change', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    expect(getDashboardSnapshotV2).not.toHaveBeenCalled()
    await wrapper.find('#usage-tab-groups').trigger('click')
    await flushPromises()
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      include_trend: true, include_model_stats: false, include_group_stats: true,
    }))
    await wrapper.find('#usage-tab-analysis').trigger('click')
    await flushPromises()
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    await wrapper.find('#usage-tab-usage').trigger('click')
    ;(wrapper.vm as any).filters.api_key_id = 1
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(1)
    await wrapper.find('#usage-tab-groups').trigger('click')
    await flushPromises()
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(2)
    expect(getDashboardSnapshotV2).toHaveBeenLastCalledWith(expect.objectContaining({ api_key_id: 1 }))
    wrapper.unmount()
  })

  it('ignores chart data from before a hidden-tab filter change', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    let resolveOld!: (value: unknown) => void
    getDashboardSnapshotV2.mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve }))
    await wrapper.find('#usage-tab-groups').trigger('click')
    await wrapper.find('#usage-tab-usage').trigger('click')
    ;(wrapper.vm as any).filters.api_key_id = 1
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()
    const latest = [{ group_id: 1, name: 'Current group', requests: 2 }]
    getDashboardSnapshotV2.mockResolvedValueOnce({ trend: [], groups: latest })
    await wrapper.find('#usage-tab-groups').trigger('click')
    await flushPromises()
    resolveOld({ trend: [], groups: [{ group_id: 2, name: 'Old group', requests: 999 }] })
    await flushPromises()
    expect((wrapper.vm as any).groupStats).toEqual(latest)
    wrapper.unmount()
  })

  it('includes API keys after the first page in both record filters and queries by the selected key', async () => {
    const firstPageKeys = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1,
      name: `key-${index + 1}`,
    }))
    const laterKey = { id: 101, name: 'key-from-second-page' }
    list
      .mockResolvedValueOnce({ items: firstPageKeys, total: 101, page: 1, page_size: 100, pages: 2 })
      .mockResolvedValueOnce({ items: [laterKey], total: 101, page: 2, page_size: 100, pages: 2 })

    const wrapper = mountUsageView()
    await flushPromises()

    expect(list.mock.calls).toEqual([[1, 100], [2, 100]])
    const usageKeySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(usageKeySelect.props('options')).toHaveLength(102)
    expect(usageKeySelect.props('options')).toContainEqual({ value: laterKey.id, label: laterKey.name })

    query.mockClear()
    usageKeySelect.vm.$emit('update:modelValue', laterKey.id)
    usageKeySelect.vm.$emit('change', laterKey.id)
    await flushPromises()

    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ api_key_id: laterKey.id, page: 1 }),
      expect.anything()
    )

    await wrapper.findAll('button').find((button) => button.text() === 'Error records')!.trigger('click')
    await flushPromises()

    const errorKeySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(errorKeySelect.props('options')).toHaveLength(102)
    expect(errorKeySelect.props('options')).toContainEqual({ value: laterKey.id, label: laterKey.name })

    listMyErrorRequests.mockClear()
    errorKeySelect.vm.$emit('update:modelValue', laterKey.id)
    errorKeySelect.vm.$emit('change', laterKey.id)
    await flushPromises()

    expect(listMyErrorRequests).toHaveBeenCalledWith(
      expect.objectContaining({ api_key_id: laterKey.id, page: 1 })
    )
    expect(list).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('does not request another API key page when the user has no keys', async () => {
    list.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 0 })

    const wrapper = mountUsageView()
    await flushPromises()

    expect(list.mock.calls).toEqual([[1, 100]])
    const keySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(keySelect.props('options')).toEqual([{ value: null, label: 'All API Keys' }])
    expect(getAvailable).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('stops loading API keys when a later page is empty despite an outdated page count', async () => {
    const firstPageKeys = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1,
      name: `key-${index + 1}`,
    }))
    list
      .mockResolvedValueOnce({ items: firstPageKeys, total: 201, page: 1, page_size: 100, pages: 3 })
      .mockResolvedValueOnce({ items: [], total: 201, page: 2, page_size: 100, pages: 3 })

    const wrapper = mountUsageView()
    await flushPromises()

    expect(list.mock.calls).toEqual([[1, 100], [2, 100]])
    const keySelect = wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
    )!
    expect(keySelect.props('options')).toHaveLength(101)
    expect(keySelect.props('options')).toContainEqual({ value: 100, label: 'key-100' })
    wrapper.unmount()
  })

  it('propagates and resets the native compaction filter across page requests', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    await wrapper.find('#usage-tab-analysis').trigger('click')
    await flushPromises()

    expect((wrapper.vm as any).compactionOptions).toEqual([
      { value: null, label: 'All Requests' },
      { value: true, label: 'Compaction Only' },
    ])

    query.mockClear()
    getStats.mockClear()
    getDashboardModels.mockClear()
    getDashboardSnapshotV2.mockClear()

    ;(wrapper.vm as any).filters.native_compaction_v2 = true
    ;(wrapper.vm as any).applyFilters()
    await flushPromises()

    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ native_compaction_v2: true }),
      expect.anything()
    )
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))
    expect(getDashboardModels).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: true }))

    query.mockClear()
    getStats.mockClear()
    getDashboardModels.mockClear()
    getDashboardSnapshotV2.mockClear()

    ;(wrapper.vm as any).resetFilters()
    await flushPromises()

    expect((wrapper.vm as any).filters.native_compaction_v2).toBeNull()
    expect(query).toHaveBeenCalledWith(
      expect.objectContaining({ native_compaction_v2: null }),
      expect.anything()
    )
    expect(getStats).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
    expect(getDashboardModels).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
    expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({ native_compaction_v2: null }))
  })

  it('exports csv with current filters and without admin-only fields', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    ;(wrapper.vm as any).filters.native_compaction_v2 = true

    let exportedBlob: Blob | null = null
    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn((blob: Blob | MediaSource) => {
      exportedBlob = blob as Blob
      return 'blob:usage-export'
    }) as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(exportedBlob).not.toBeNull()
    expect(query).toHaveBeenCalledWith(expect.objectContaining({
      page_size: 100,
      sort_by: 'created_at',
      sort_order: 'desc',
      native_compaction_v2: true,
    }))
    expect(clickSpy).toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalled()
    expect(csvContent.startsWith('\uFEFF')).toBe(true)
    expect(csvContent.slice(1)).toBe([
      'Time,API Key Name,Model,Reasoning Effort,Inbound Endpoint,IP Address,Type,Billing Mode,Input Tokens,Output Tokens,Cache Read Tokens,Cache Creation Tokens,Rate Multiplier,Billed Cost,Original Cost,First Token (ms),Duration (ms)',
      '2026-03-08T00:00:00Z,demo-key,gpt-5.4,-,,203.0.113.10,Sync,Token,4057,101,278272,4,1,0.09288300,0.09288300,12,345',
    ].join('\n'))
    expect(csvContent).not.toContain('Cache Rate')
    expect(csvContent).toContain('IP Address')
    expect(csvContent).toContain('203.0.113.10')
    expect(csvContent).toContain('Billed Cost')
    expect(csvContent).toContain('Original Cost')
    expect(csvContent).not.toContain('Upstream Endpoint')
    expect(csvContent).not.toContain('account_cost')
    expect(csvContent).not.toContain('account_rate_multiplier')

    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
  })

  it('includes cache rate in csv only when users are allowed to see it', async () => {
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true, usage_show_cache_rate_to_users: true }
    const wrapper = mountUsageView()
    await flushPromises()

    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export') as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(csvContent).toContain('Cache Rate')
    expect(csvContent).toContain('98.6%')
    expect(csvContent).not.toContain('Upstream Response Model')
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true }
    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
    wrapper.unmount()
  })

  it('includes the upstream response model in csv only when users are allowed to see it', async () => {
    query.mockResolvedValue({
      items: [{ ...usageLog, upstream_response_model: 'gpt-5.4-actual' }],
      total: 1,
      pages: 1,
    })
    appStoreState.cachedPublicSettings = { usage_show_upstream_response_model_to_users: true }
    const wrapper = mountUsageView()
    await flushPromises()

    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export') as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(csvContent).toContain('Upstream Response Model')
    expect(csvContent).toContain('gpt-5.4,gpt-5.4-actual,')
    expect(csvContent).not.toContain('upstream_model')
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true }
    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
    wrapper.unmount()
  })

  it('keeps formula-injection protection for dangerous exported values', async () => {
    query.mockResolvedValue({
      items: [{ ...usageLog, api_key: { name: '-1+1' } }],
      total: 1,
      pages: 1,
    })
    const wrapper = mountUsageView()
    await flushPromises()

    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export') as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(csvContent).toContain(',"\'-1+1",gpt-5.4,-,')
    expect(showSuccess).toHaveBeenCalled()

    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
    wrapper.unmount()
  })
  it('keeps the initial filters, sort, and filename while exporting multiple pages', async () => {
    const pageResponse = { items: [usageLog], total: 101, pages: 2 }
    query.mockResolvedValue(pageResponse)
    const wrapper = mountUsageView()
    await flushPromises()

    const datePicker = wrapper.findComponent(DateRangePicker)
    datePicker.vm.$emit('change', { startDate: '2026-03-01', endDate: '2026-03-08', preset: null })
    await flushPromises()

    let resolveFirstPage!: (value: typeof pageResponse) => void
    const firstPage = new Promise<typeof pageResponse>((resolve) => { resolveFirstPage = resolve })
    query.mockClear()
    query.mockImplementation((params, options) =>
      !options && params.page === 1 ? firstPage : Promise.resolve(pageResponse)
    )
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export')
    window.URL.revokeObjectURL = vi.fn()
    let filename = ''
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () {
      filename = this.download
    })

    try {
      await wrapper.findAll('button').find((button) => button.text() === 'Export CSV')!.trigger('click')
      const initialParams = { ...query.mock.calls[0][0] }
      expect(initialParams).toMatchObject({
        page: 1, page_size: 100, start_date: '2026-03-01', end_date: '2026-03-08',
        sort_by: 'created_at', sort_order: 'desc',
      })

      const keySelect = wrapper.findAllComponents(Select).find((select) =>
        select.props('options').some((option: SelectOption) => option.label === 'All API Keys')
      )!
      keySelect.vm.$emit('update:modelValue', 1)
      keySelect.vm.$emit('change', 1)
      datePicker.vm.$emit('change', { startDate: '2026-04-01', endDate: '2026-04-08', preset: null })
      wrapper.findComponent(UsageTable).vm.$emit('sort', 'actual_cost', 'asc')
      await flushPromises()
      expect(query).toHaveBeenCalledWith(expect.objectContaining({
        api_key_id: 1, start_date: '2026-04-01', end_date: '2026-04-08',
        sort_by: 'actual_cost', sort_order: 'asc',
      }), expect.anything())

      resolveFirstPage(pageResponse)
      await flushPromises()

      const exportCalls = query.mock.calls.filter((call) => call.length === 1)
      expect.soft(exportCalls).toEqual([[initialParams], [{ ...initialParams, page: 2 }]])
      expect.soft(filename).toBe('usage_2026-03-01_to_2026-03-08.csv')
      expect(showSuccess).toHaveBeenCalledWith('Export success')
      expect(showError).not.toHaveBeenCalled()
    } finally {
      window.URL.createObjectURL = originalCreateObjectURL
      window.URL.revokeObjectURL = originalRevokeObjectURL
      clickSpy.mockRestore()
      wrapper.unmount()
    }
  })

  it('exports historical image rows with image billing mode derived from image_count', async () => {
    query.mockResolvedValue({
      items: [
        {
          ...usageLog,
          request_id: 'req-user-export-legacy-image',
          actual_cost: 0.2,
          total_cost: 0.2,
          input_cost: 0,
          output_cost: 0,
          cache_creation_cost: 0,
          cache_read_cost: 0,
          input_tokens: 0,
          output_tokens: 0,
          cache_creation_tokens: 0,
          cache_read_tokens: 0,
          image_count: 1,
          model: 'gpt-image-2',
          billing_mode: null,
          ip_address: null,
        },
      ],
      total: 1,
      pages: 1,
    })

    const wrapper = mountUsageView()
    await flushPromises()

    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export') as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(csvContent).toContain('Billing Mode')
    expect(csvContent).toContain('Image')
    expect(csvContent).not.toContain(',Token,0,0,0,0,')

    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
  })
})

describe('UsageView subscription feature flag', () => {
  afterEach(() => {
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true }
  })

  function billingTypeSelect(wrapper: ReturnType<typeof mountUsageView>) {
    return wrapper.findAllComponents(Select).find((select) =>
      select.props('options').some((option: SelectOption) => option.label === 'Subscription')
    )
  }

  it('offers the balance / subscription billing-type filter by default', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    expect(billingTypeSelect(wrapper)).toBeDefined()
    expect(wrapper.text()).toContain('Billing type')
    wrapper.unmount()
  })

  it('hides the billing-type filter entirely when subscriptions are disabled', async () => {
    appStoreState.cachedPublicSettings = { allow_user_view_error_requests: true, subscription_enabled: false }

    const wrapper = mountUsageView()
    await flushPromises()

    expect(billingTypeSelect(wrapper)).toBeUndefined()
    expect(wrapper.text()).not.toContain('Billing type')
    wrapper.unmount()
  })
})
