import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

enableAutoUnmount(afterEach)

vi.mock('@/api/admin/accountTokenGuardV2', () => ({
  listTokenGuardV2Accounts: vi.fn().mockResolvedValue({ accounts: [] })
}))

const { listAccounts, getOpenAIAccountSummary } = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  getOpenAIAccountSummary: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getManagementCapabilities: vi.fn().mockResolvedValue({ web_search_enabled: false, account_quota_notify_enabled: false }),
      list: listAccounts,
      listWithEtag: vi.fn().mockResolvedValue({ notModified: true, etag: null, data: null }),
      getBatchTodayStats: vi.fn().mockResolvedValue({ stats: {} }),
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true }),
      getOpenAIAccountSummary
    },
    proxies: { getAll: vi.fn().mockResolvedValue([]) },
    groups: { getAll: vi.fn().mockResolvedValue([{ id: 7, name: 'OpenAI', platform: 'openai' }]) }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showWarning: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isAdmin: true, isSimpleMode: false })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const AccountTableFiltersStub = defineComponent({
  emits: ['update:filters', 'change'],
  template: '<div><button data-test="set-openai" @click="$emit(\'update:filters\', { platform: \'openai\' }); $emit(\'change\')">openai</button><button data-test="set-openai-group" @click="$emit(\'update:filters\', { group: \'7\' }); $emit(\'change\')">openai group</button></div>'
})

const OpenAIAccountSummaryCardsStub = defineComponent({
  emits: ['select'],
  template: '<button data-test="select-5h" @click="$emit(\'select\', { type: \'oauth\', quota_window: \'5h\' })">5h</button>'
})

const mountView = () => mount(AccountsView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
      DataTable: true,
      Pagination: true,
      ConfirmDialog: true,
      AccountTableActions: { template: '<div><slot name="after" /></div>' },
      AccountTableFilters: AccountTableFiltersStub,
      OpenAIAccountSummaryCards: OpenAIAccountSummaryCardsStub,
      AccountBulkActionsBar: true,
      AccountActionMenu: true,
      CredentialOpsDrawer: true,
      NewFeatureBadge: true,
      ImportDataModal: true,
      ReAuthAccountModal: true,
      AccountTestModal: true,
      AccountStatsModal: true,
      ScheduledTestsPanel: true,
      SyncFromCrsModal: true,
      TempUnschedStatusModal: true,
      ErrorPassthroughRulesModal: true,
      TLSFingerprintProfilesModal: true,
      CreateAccountModal: true,
      EditAccountModal: true,
      BulkEditAccountModal: true,
      PlatformTypeBadge: true,
      AccountCapacityCell: true,
      AccountStatusIndicator: true,
      AccountTodayStatsCell: true,
      AccountGroupsCell: true,
      AccountUsageCell: true,
      UpstreamBillingRateCell: true,
      HelpTooltip: true,
      Icon: true
    }
  }
})

describe('admin AccountsView OpenAI summary filters', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    getOpenAIAccountSummary.mockReset().mockResolvedValue({
      accounts: { oauth: 0, apikey: 0 },
      oauth_status: { normal: 0, rate_limited: 0, error: 0 },
      oauth_quota: {
        '5h': { remaining_cost: null, estimated_cost: null },
        '7d': { remaining_cost: null, estimated_cost: null }
      },
      concurrency: { oauth: { used: 0, total: 0 }, apikey: { used: 0, total: 0 } }
    })
  })

  it('applies and toggles off the 5h quota filter', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="set-openai"]').trigger('click')
    await wrapper.get('[data-test="select-5h"]').trigger('click')
    await flushPromises()

    expect(listAccounts).toHaveBeenLastCalledWith(
      1,
      20,
      expect.objectContaining({ platform: 'openai', type: 'oauth', status: '', quota_window: '5h' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    await wrapper.get('[data-test="select-5h"]').trigger('click')
    await flushPromises()

    expect(listAccounts).toHaveBeenLastCalledWith(
      1,
      20,
      expect.objectContaining({ platform: 'openai', type: 'oauth', status: '', quota_window: '' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('shows the summary for an OpenAI group without a platform filter', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="set-openai-group"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="select-5h"]').exists()).toBe(true)
    await vi.waitFor(() => {
      expect(getOpenAIAccountSummary).toHaveBeenLastCalledWith(expect.objectContaining({ group: '7' }))
    })
  })
})
