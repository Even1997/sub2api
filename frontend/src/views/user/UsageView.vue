<template>
  <AppLayout class="personal-usage-layout">
    <div class="personal-usage">
      <div v-if="activeTab !== 'traffic'" class="usage-toolbar">
        <div v-if="activeTab !== 'errors'" class="usage-group-tabs" role="group" :aria-label="t('usage.personal.modelProvider')">
          <button type="button" :aria-pressed="!filters.model_provider" :class="{ selected: !filters.model_provider }" @click="selectProvider(undefined)">{{ t('common.all') }}</button>
          <button
            v-for="provider in modelProviders"
            :key="provider.value"
            type="button"
            :title="provider.label"
            :aria-label="provider.label"
            :aria-pressed="filters.model_provider === provider.value"
            :class="{ selected: filters.model_provider === provider.value }"
            @click="selectProvider(provider.value)"
          ><PlatformIcon :platform="provider.value" size="md" :class="platformIconClass(provider.value)" /></button>
        </div>
        <div class="usage-primary-filters">
          <template v-if="activeTab !== 'errors'">
            <Select v-model="filters.model" :options="modelOptions" searchable :placeholder="t('admin.usage.allModels')" :aria-label="t('usage.model')" @change="applyFilters()" />
            <Select v-model="filters.api_key_id" :options="apiKeyOptions" :placeholder="t('usage.allApiKeys')" :aria-label="t('usage.apiKeyFilter')" @change="applyFilters()" />
          </template>
          <template v-else>
            <Select v-model="errorFilter.model" :options="errorModelOptions" searchable creatable clearable :placeholder="t('usage.errors.modelPlaceholder')" :aria-label="t('usage.errors.model')" @change="applyErrorFilters" />
            <Select v-model="errorFilter.api_key_id" :options="errorKeyOptions" :aria-label="t('usage.errors.keyName')" @change="applyErrorFilters" />
          </template>
          <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" allow-all @change="onDateRangeChange" />
          <button type="button" class="usage-icon-button" :title="t('common.refresh')" :aria-label="t('common.refresh')" :disabled="pageLoading" @click="refreshData"><Icon name="refresh" size="sm" :class="{ 'animate-spin': pageLoading }" /></button>
        </div>
      </div>

      <div v-if="activeTab !== 'traffic'" class="usage-summary" :aria-busy="statsLoading">
        <div v-for="metric in summaryMetrics" :key="metric.label" class="usage-stat" :title="`${startDate ? `${startDate} ~ ${endDate}` : t('common.all')}${statsLoading ? '' : `: ${metric.exactValue ?? metric.value}`}`">
          <div class="usage-stat-label" :title="metric.hint">
            {{ metric.label }}
            <span v-if="metric.hint && metric.showHintIcon" tabindex="0" :title="metric.hint" :aria-label="metric.hint"><Icon name="questionCircle" size="xs" /></span>
          </div>
          <div class="usage-stat-value" :style="{ '--usage-value-length': metric.value.length }">{{ statsLoading && !usageStats ? '-' : metric.value }}</div>
        </div>
        <button type="button" class="usage-more-metrics" :aria-expanded="showMoreMetrics" aria-controls="usage-extra-metrics" @click="showMoreMetrics = !showMoreMetrics">{{ t('usage.personal.moreMetrics') }}<Icon :name="showMoreMetrics ? 'chevronUp' : 'chevronDown'" size="sm" /></button>
      </div>
      <p v-if="!statsLoading && usageStats?.history_incomplete" class="usage-history-notice" role="status">
        <Icon name="infoCircle" size="sm" />
        {{ usageStats.records_available_from ? t('usage.personal.retainedHistory', { date: formatLocalDate(new Date(usageStats.records_available_from)) }) : t('usage.personal.missingHistory') }}
      </p>
      <div v-if="showMoreMetrics" id="usage-extra-metrics" class="usage-extra-metrics">
        <span>{{ t('usage.in') }} <strong>{{ formatCompactNumber(usageStats?.total_input_tokens) }}</strong></span>
        <span>{{ t('usage.out') }} <strong>{{ formatCompactNumber(usageStats?.total_output_tokens) }}</strong></span>
        <span>{{ t('usage.cacheCreationTokensLabel') }} <strong>{{ formatCompactNumber(usageStats?.total_cache_creation_tokens) }}</strong></span>
        <span>{{ t('usage.cacheReadTokensLabel') }} <strong>{{ formatCompactNumber(usageStats?.total_cache_read_tokens) }}</strong></span>
        <span>{{ t('usage.avgDuration') }} <strong>{{ ((usageStats?.average_duration_ms || 0) / 1000).toFixed(2) }}s</strong></span>
        <span>{{ t('usage.standardCost') }} <strong>${{ (usageStats?.total_cost || 0).toFixed(4) }}</strong></span>
      </div>

      <UsageHeatmap
        :data="heatmapData"
        :coverage="heatmapCoverage"
        :start-date="heatmapStartDate"
        :end-date="heatmapEndDate"
        :loading="heatmapLoading"
        :error="heatmapError"
        :can-go-next="heatmapEndDate < today"
        :selected-date="startDate === endDate ? startDate : ''"
        @select="selectHeatmapDate"
        @navigate="navigateHeatmap"
        @retry="loadHeatmap"
        @details="hourlyOpen = true"
      />
      <UsageTrendChart
        v-if="startDate && startDate === endDate"
        :data="trendData"
        :start-date="startDate"
        :end-date="endDate"
        :granularity="granularity"
        :loading="chartsLoading"
        :error="chartsError"
        @retry="loadChartData"
      />

      <div class="usage-records-heading">
        <div class="usage-view-tabs" role="tablist" :aria-label="t('usage.title')">
          <button v-for="tab in usageTabs" :id="`usage-tab-${tab.value}`" :key="tab.value" type="button" role="tab" :aria-selected="activeTab === tab.value" aria-controls="usage-view-panel" :class="{ selected: activeTab === tab.value }" @click="selectTab(tab.value)">{{ tab.label }}</button>
        </div>
        <div class="usage-record-actions">
          <button type="button" class="usage-icon-button" :title="t('common.refresh')" :aria-label="t('common.refresh')" :disabled="pageLoading" @click="refreshData"><Icon name="refresh" size="sm" :class="{ 'animate-spin': pageLoading }" /></button>
          <button type="button" class="usage-filter-button" :class="{ selected: showAdvancedFilters }" :aria-expanded="showAdvancedFilters" aria-controls="usage-advanced-filters" @click="showAdvancedFilters = !showAdvancedFilters"><Icon name="filter" size="sm" />{{ t('usage.personal.filters') }}<span v-if="advancedFilterCount" class="usage-filter-count">{{ advancedFilterCount }}</span></button>
          <template v-if="activeTab === 'usage' || activeTab === 'errors'">
            <div class="relative" ref="columnDropdownRef">
              <button type="button" data-testid="usage-column-settings" class="usage-icon-button" :aria-label="t('admin.users.columnSettings')" :title="t('admin.users.columnSettings')" :aria-expanded="showColumnDropdown" @click="showColumnDropdown = !showColumnDropdown"><Icon name="grid" size="sm" /></button>
              <div v-if="showColumnDropdown" class="usage-column-menu" role="group" :aria-label="t('admin.users.columnSettings')" @keydown.esc="showColumnDropdown = false">
                <button v-for="col in currentToggleableColumns" :key="col.key" type="button" :data-testid="`usage-column-toggle-${col.key}`" role="checkbox" :aria-checked="isCurrentColumnVisible(col.key)" @click="toggleCurrentColumn(col.key)">
                  <span>{{ col.label }}</span><Icon v-if="isCurrentColumnVisible(col.key)" name="check" size="sm" />
                </button>
              </div>
            </div>
            <button v-if="activeTab === 'usage'" type="button" class="usage-export-button" :disabled="exporting" @click="exportToCSV"><Icon name="download" size="sm" /><span>{{ exporting ? t('usage.exporting') : t('usage.exportCsv') }}</span></button>
          </template>
        </div>
      </div>

      <div v-if="activeTab !== 'traffic'" v-show="showAdvancedFilters" id="usage-advanced-filters" class="usage-advanced-filters">
        <div class="flex flex-wrap items-end justify-between gap-4">
          <div v-if="activeTab === 'errors'" class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('usage.errors.category') }}</label>
              <Select v-model="errorFilter.category" :options="errorCategoryOptions" @change="applyErrorFilters" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.errors.status') }}</label>
              <Select v-model="errorFilter.status_code" :options="errorStatusOptions" @change="applyErrorFilters" />
            </div>
          </div>
          <div v-else class="flex flex-1 flex-wrap items-end gap-4">
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.group') }}</label>
              <Select v-model="filters.group_id" :options="groupOptions" searchable :placeholder="t('admin.usage.allGroups')" @change="applyFilters()" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.type') }}</label>
              <Select v-model="filters.request_type" :options="requestTypeOptions" :placeholder="t('admin.usage.allTypes')" @change="applyFilters()" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[180px]">
              <label class="input-label">{{ t('usage.compactionFilter') }}</label>
              <Select v-model="filters.native_compaction_v2" :options="compactionOptions" @change="applyFilters()" />
            </div>
            <div v-if="subscriptionFeatureEnabled" class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.billingType') }}</label>
              <Select v-model="filters.billing_type" :options="billingTypeOptions" @change="applyFilters()" />
            </div>
            <div class="w-full sm:w-auto sm:min-w-[200px]">
              <label class="input-label">{{ t('admin.usage.billingMode') }}</label>
              <Select v-model="filters.billing_mode" :options="billingModeOptions" @change="applyFilters()" />
            </div>
          </div>

          <div class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
            <button type="button" @click="resetFilters" class="btn btn-secondary">
              <Icon name="refresh" size="sm" />
              {{ t('common.reset') }}
            </button>
          </div>
        </div>
      </div>

      <div id="usage-view-panel" class="usage-records" role="tabpanel" :aria-labelledby="`usage-tab-${activeTab}`">
      <template v-if="activeTab === 'usage'">
        <UsageTable
          :data="usageLogs"
          :loading="loading"
          :columns="visibleColumns"
          :server-side-sort="true"
          :show-account-billing="false"
          :show-upstream-endpoint="false"
          compact
          flat
          cache-rate-audience="user"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
          @ipGeoBatchFailed="handleIpGeoBatchFailed"
        />

        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>

      <UserErrorRequestsTable
        v-else-if="activeTab === 'errors' && errorViewEnabled"
        :rows="errorRows"
        :total="errorTotal"
        :loading="errorLoading"
        :page="errorPage"
        :page-size="errorPageSize"
        :visible-column-keys="errVisibleColumnKeys"
        @sort="onErrorSort"
        @update:page="onErrorPage"
        @update:pageSize="onErrorPageSize"
        @ipGeoBatchFailed="handleIpGeoBatchFailed"
      />
      <ModelDistributionChart
        v-else-if="activeTab === 'models'"
        v-model:metric="modelDistributionMetric"
        :model-stats="requestedModelStats"
        :loading="modelStatsLoading"
        :show-source-toggle="false"
        :show-metric-toggle="true"
        :enable-breakdown="false"
        :show-account-cost="false"
        :start-date="startDate"
        :end-date="endDate"
      />
      <GroupDistributionChart
        v-else-if="activeTab === 'groups'"
        v-model:metric="groupDistributionMetric"
        :group-stats="groupStats"
        :loading="chartsLoading"
        :show-metric-toggle="true"
        :enable-breakdown="false"
        :show-account-cost="false"
        :start-date="startDate"
        :end-date="endDate"
      />
      <div v-else-if="activeTab === 'analysis'" class="space-y-4">
        <div class="flex items-center justify-end gap-2 text-sm">
          <span>{{ t('admin.dashboard.granularity') }}</span>
          <Select v-model="granularity" class="w-28" :options="granularityOptions" :aria-label="t('admin.dashboard.granularity')" @change="loadChartData" />
        </div>
        <div class="grid gap-4 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="false"
            :show-metric-toggle="true"
            :enable-breakdown="false"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>
      <TrafficPanel v-else-if="activeTab === 'traffic'" :api-key-options="apiKeyOptions" />
      </div>
    </div>
    <HourlyUsageDrawer :show="hourlyOpen" :initial-date="startDate === endDate ? startDate : today" :filters="normalizedFilters" @close="hourlyOpen = false" />
  </AppLayout>

</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, h, onMounted, onUnmounted, reactive, ref, watch, type AsyncComponentLoader } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { keysAPI, usageAPI, userGroupsAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import HourlyUsageDrawer from '@/components/user/HourlyUsageDrawer.vue'
import type { HourlyUsageReport } from '@/api/usage'
import UsageHeatmap from '@/components/user/UsageHeatmap.vue'
import TrafficPanel from '@/components/usage/TrafficPanel.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCompactNumber, formatReasoningEffort } from '@/utils/format'
import { formatUsageCacheRate, showUsageCacheRate } from '@/utils/usageCacheRate'
import { platformIconClass } from '@/utils/platformColors'
import { showUpstreamResponseModelToUsers } from '@/utils/upstreamResponseModel'
import { getBillingModeLabel, getDisplayBillingMode as resolveDisplayBillingMode } from '@/utils/billingMode'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import type {
  ApiKey,
  EndpointStat,
  Group,
  GroupStat,
  ModelStat,
  TrendDataPoint,
  UsageLog,
  UsageQueryParams,
  UsageStatsResponse,
  UserErrorRequest,
} from '@/types'
import type { Column } from '@/components/common/types'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'

const panelLoading = {
  inheritAttrs: false,
  render: () => h('div', { class: 'flex h-64 items-center justify-center', 'aria-busy': 'true' }, h(LoadingSpinner)),
}
const lazyPanel = (loader: AsyncComponentLoader) => defineAsyncComponent({ loader, loadingComponent: panelLoading, delay: 0 })
const UsageTrendChart = lazyPanel(() => import('@/components/user/UsageTrendChart.vue'))
const ModelDistributionChart = lazyPanel(() => import('@/components/charts/ModelDistributionChart.vue'))
const GroupDistributionChart = lazyPanel(() => import('@/components/charts/GroupDistributionChart.vue'))
const EndpointDistributionChart = lazyPanel(() => import('@/components/charts/EndpointDistributionChart.vue'))
const TokenUsageTrend = lazyPanel(() => import('@/components/charts/TokenUsageTrend.vue'))
const UserErrorRequestsTable = lazyPanel(() => import('@/components/user/UserErrorRequestsTable.vue'))

const { t } = useI18n()
const appStore = useAppStore()

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type UsageTab = 'usage' | 'errors' | 'models' | 'groups' | 'analysis' | 'traffic'

const usageStats = ref<UsageStatsResponse | null>(null)
const usageLogs = ref<UsageLog[]>([])
const trendData = ref<TrendDataPoint[]>([])
const requestedModelStats = ref<ModelStat[]>([])
const groupStats = ref<GroupStat[]>([])
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])

const loading = ref(false)
const chartsLoading = ref(false)
const chartsError = ref(false)
const modelStatsLoading = ref(false)
const endpointStatsLoading = ref(false)
const exporting = ref(false)
const statsLoading = ref(true)
const showMoreMetrics = ref(false)
const showAdvancedFilters = ref(false)
const hourlyOpen = ref(false)
const heatmapCoverage = ref<HourlyUsageReport | null>(null)
const heatmapData = ref<TrendDataPoint[]>([])
const heatmapLoading = ref(false)
const heatmapError = ref(false)
const today = formatLocalDate(new Date())
const heatmapEndDate = ref(today)
const heatmapStartDate = computed(() => {
  const date = new Date(`${heatmapEndDate.value}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 6) % 7 - 52 * 7)
  return date.toISOString().slice(0, 10)
})
let heatmapReqSeq = 0
const pageLoading = computed(() => loading.value || statsLoading.value || chartsLoading.value || modelStatsLoading.value || heatmapLoading.value || errorLoading.value)
const summaryMetrics = computed(() => [
  { label: t('usage.totalCost'), value: usageStats.value ? `$${usageStats.value.total_actual_cost >= 1_000_000 ? formatCompactNumber(usageStats.value.total_actual_cost) : usageStats.value.total_actual_cost.toFixed(2)}` : '-', exactValue: usageStats.value ? `$${usageStats.value.total_actual_cost.toFixed(2)}` : '-' },
  { label: t('usage.personal.totalCost'), value: usageStats.value ? `$${usageStats.value.total_cost >= 1_000_000 ? formatCompactNumber(usageStats.value.total_cost) : usageStats.value.total_cost.toFixed(2)}` : '-', exactValue: usageStats.value ? `$${usageStats.value.total_cost.toFixed(2)}` : '-' },
  { label: t('usage.totalRequests'), value: usageStats.value ? usageStats.value.total_requests >= 1_000_000 ? formatCompactNumber(usageStats.value.total_requests) : usageStats.value.total_requests.toLocaleString() : '-', exactValue: usageStats.value?.total_requests.toLocaleString() },
  { label: t('usage.totalTokens'), value: usageStats.value ? formatCompactNumber(usageStats.value.total_tokens) : '-', exactValue: usageStats.value?.total_tokens.toLocaleString(), hint: t('usage.personal.tokensHint'), showHintIcon: true },
])
const errorRows = ref<UserErrorRequest[]>([])
const errorLoading = ref(false)
const errorPage = ref(1)
const errorPageSize = ref(20)
const errorSortBy = ref('created_at')
const errorSortOrder = ref<'asc' | 'desc'>('desc')
const errorTotal = ref(0)
const errorFilter = ref<{ model: string | null; category: string; api_key_id: number | null; status_code: number | null }>({
  model: '',
  category: '',
  api_key_id: null,
  status_code: null,
})

const errorKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allKeys') },
  ...apiKeys.value.map((k) => ({ value: k.id, label: k.name })),
])

// 模型候选取自当前已加载错误中出现过的模型；creatable 允许输入任意片段做后端模糊。
const errorModelOptions = computed<SelectOption[]>(() => {
  const seen = new Set<string>()
  const opts: SelectOption[] = []
  for (const r of errorRows.value) {
    if (r.model && !seen.has(r.model)) {
      seen.add(r.model)
      opts.push({ value: r.model, label: r.model })
    }
  }
  return opts
})

const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

// 状态码候选用固定常用列表(与管理端 UsageFilters 共用常量),不受当前页数据限制:
// 后端 status_code 过滤对全量生效,若只列当前页出现过的码,用户就选不到仅在后续页的码。
const errorStatusOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const applyErrorFilters = () => {
  errorPage.value = 1
  void loadErrors()
}

let abortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0

function formatLocalDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const getLast24HoursRangeDates = () => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatLocalDate(start), end: formatLocalDate(end) }
}

const getGranularityForRange = (start: string, end: string): 'day' | 'hour' => {
  if (!start || !end) return 'day'
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  return Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24)) <= 1 ? 'hour' : 'day'
}

const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)
const granularity = ref<'day' | 'hour'>(getGranularityForRange(startDate.value, endDate.value))

const modelDistributionMetric = ref<DistributionMetric>('tokens')
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const activeTab = ref<UsageTab>('usage')
const errorViewEnabled = computed(() => appStore.cachedPublicSettings?.allow_user_view_error_requests ?? false)
const usageTabs = computed<{ value: UsageTab; label: string }[]>(() => [
  { value: 'usage', label: t('usage.personal.requestLogs') },
  { value: 'groups', label: t('admin.usage.group') },
  { value: 'models', label: t('usage.model') },
  { value: 'analysis', label: t('usage.personal.analysis') },
  ...(errorViewEnabled.value ? [{ value: 'errors' as const, label: t('usage.tabs.errors') }] : []),
  { value: 'traffic', label: t('traffic.title') },
])
const includeCacheRate = computed(() => showUsageCacheRate(appStore.cachedPublicSettings, 'user'))
const includeUpstreamResponseModel = computed(() => showUpstreamResponseModelToUsers(appStore.cachedPublicSettings))

const filters = ref<UsageQueryParams>({
  start_date: startDate.value,
  end_date: endDate.value,
  request_type: undefined,
  native_compaction_v2: null,
  billing_type: null,
  billing_mode: null,
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc',
})

const granularityOptions = computed<SelectOption[]>(() => [
  { value: 'day', label: t('admin.dashboard.day') },
  { value: 'hour', label: t('admin.dashboard.hour') },
])
const requestTypeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
])
const compactionOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allCompactionTypes') },
  { value: true, label: t('usage.compactionOnly') },
])
// 订阅功能关闭后只剩余额计费，「计费类型」筛选（余额/订阅）失去意义，整块隐藏。
const subscriptionFeatureEnabled = computed(() => resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription))
const billingTypeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') },
])
const billingModeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') },
])

const apiKeys = ref<ApiKey[]>([])
const groups = ref<Group[]>([])
const modelOptionValues = ref<string[]>([])
const modelProviders = [
  { value: 'openai', label: 'OpenAI', pattern: /(^|[/ :])(gpt|chatgpt|o[134]([-. /:]|$)|codex|dall-e|whisper|tts-1|text-embedding|text-moderation|sora)/i },
  { value: 'anthropic', label: 'Claude', pattern: /(^|[/ :])claude/i },
  { value: 'grok', label: 'Grok', pattern: /(^|[/ :])grok/i },
  { value: 'gemini', label: 'Gemini', pattern: /(^|[/ :])(gemini|gemma|learnlm|imagen|veo)/i },
] as const

const apiKeyOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.allApiKeys') },
  ...apiKeys.value.map((key) => ({ value: key.id, label: key.name })),
])
const groupOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allGroups') },
  ...groups.value.map((group) => ({ value: group.id, label: group.name })),
])
const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...modelOptionValues.value.filter((model) => !filters.value.model_provider || modelProviders.find((provider) => provider.value === filters.value.model_provider)?.pattern.test(model)).map((model) => ({ value: model, label: model })),
])

const normalizedFilters = computed<UsageQueryParams>(() => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    ...filters.value,
    start_date: startDate.value || undefined,
    end_date: endDate.value || undefined,
    period: !startDate.value && !endDate.value ? 'all' : undefined,
    stream: legacyStream === null ? undefined : legacyStream,
  }
})

const advancedFilterCount = computed(() => [filters.value.group_id, filters.value.request_type, filters.value.native_compaction_v2, filters.value.billing_type, filters.value.billing_mode].filter((value) => value != null).length)

const selectProvider = (provider: UsageQueryParams['model_provider']) => {
  filters.value.model_provider = provider
  if (filters.value.model && provider && !modelProviders.find((item) => item.value === provider)?.pattern.test(filters.value.model)) filters.value.model = undefined
  applyFilters()
}
const selectTab = (tab: UsageTab) => {
  showColumnDropdown.value = false
  if (tab === 'errors') switchToErrors()
  else activeTab.value = tab
}
const selectHeatmapDate = (date: string) => {
  startDate.value = date
  endDate.value = date
  granularity.value = 'hour'
  activeTab.value = 'usage'
  applyFilters(false)
}
const navigateHeatmap = (direction: number) => {
  const date = new Date(`${heatmapStartDate.value}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + (direction < 0 ? -1 : 106 * 7 - 1))
  heatmapEndDate.value = date.toISOString().slice(0, 10) > today ? today : date.toISOString().slice(0, 10)
  void loadHeatmap()
}
const loadHeatmap = async () => {
  const seq = ++heatmapReqSeq
  heatmapLoading.value = true
  heatmapError.value = false
  try {
    const response = await usageAPI.getHourlySummary({
      ...normalizedFilters.value,
      start_date: heatmapStartDate.value,
      end_date: heatmapEndDate.value,
      period: undefined,
      granularity: 'day',
    })
    if (seq === heatmapReqSeq) { heatmapData.value = response.trend || []; heatmapCoverage.value = response }
  } catch {
    if (seq === heatmapReqSeq) {
      heatmapData.value = []
      heatmapError.value = true
    }
  } finally {
    if (seq === heatmapReqSeq) heatmapLoading.value = false
  }
}

const buildUsageListParams = (page: number, pageSize: number): UsageQueryParams => ({
  page,
  page_size: pageSize,
  ...normalizedFilters.value,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order,
})

const loadLogs = async () => {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  loading.value = true
  try {
    const res = await usageAPI.query(buildUsageListParams(pagination.page, pagination.page_size), {
      signal: controller.signal,
    })
    if (!controller.signal.aborted) {
      usageLogs.value = res.items
      pagination.total = res.total
    }
  } catch (error: any) {
    if (error?.name !== 'AbortError' && error?.code !== 'ERR_CANCELED') {
      appStore.showError(t('usage.failedToLoad'))
    }
  } finally {
    if (abortController === controller) loading.value = false
  }
}

const loadStats = async () => {
  const seq = ++statsReqSeq
  statsLoading.value = true
  endpointStatsLoading.value = true
  try {
    const stats = await usageAPI.getStats(normalizedFilters.value)
    if (seq !== statsReqSeq) return
    usageStats.value = stats
    inboundEndpointStats.value = stats.endpoints || []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } catch (error) {
    if (seq !== statsReqSeq) return
    console.error('Failed to load usage stats:', error)
    usageStats.value = null
    appStore.showError(t('usage.failedToLoad'))
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) {
      endpointStatsLoading.value = false
      statsLoading.value = false
    }
  }
}

const loadModelStats = async () => {
  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  try {
    const response = await usageAPI.getDashboardModels({
      ...normalizedFilters.value,
      model_source: 'requested',
    })
    if (seq !== modelStatsReqSeq) return
    requestedModelStats.value = response.models || []
    refreshModelOptions(response.models || [])
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    console.error('Failed to load model stats:', error)
    requestedModelStats.value = []
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const chartDataVisible = computed(() => activeTab.value === 'groups' || activeTab.value === 'analysis' || Boolean(startDate.value && startDate.value === endDate.value))
let chartDataLoaded = false

const loadChartData = async () => {
  const seq = ++chartReqSeq
  chartDataLoaded = false
  if (!chartDataVisible.value) {
    chartsLoading.value = false
    return
  }
  chartsLoading.value = true
  chartsError.value = false
  try {
    const snapshot = await usageAPI.getDashboardSnapshotV2({
      ...normalizedFilters.value,
      granularity: granularity.value,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
    })
    if (seq !== chartReqSeq) return
    trendData.value = snapshot.trend || []
    groupStats.value = snapshot.groups || []
    chartDataLoaded = true
  } catch (error) {
    if (seq !== chartReqSeq) return
    console.error('Failed to load chart data:', error)
    chartsError.value = true
    trendData.value = []
    groupStats.value = []
  } finally {
    if (seq === chartReqSeq) chartsLoading.value = false
  }
}

watch(activeTab, () => {
  if (chartDataVisible.value && !chartDataLoaded && !chartsLoading.value) void loadChartData()
})

const refreshModelOptions = (models: ModelStat[]) => {
  const current = filters.value.model
  const set = new Set(modelOptionValues.value)
  models.forEach((item) => {
    if (item.model) set.add(item.model)
  })
  if (current) set.add(current)
  modelOptionValues.value = Array.from(set).sort()
}

const applyFilters = (refreshHeatmap = true) => {
  pagination.page = 1
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  if (refreshHeatmap !== false) void loadHeatmap()
  resetErrorRows()
}

const refreshData = () => {
  void loadLogs()
  void loadStats()
  void loadModelStats()
  void loadChartData()
  void loadHeatmap()
  if (activeTab.value === 'errors') void loadErrors()
}

const resetFilters = () => {
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = {
    start_date: range.start,
    end_date: range.end,
    request_type: undefined,
    native_compaction_v2: null,
    billing_type: null,
    billing_mode: null,
  }
  granularity.value = getGranularityForRange(range.start, range.end)
  applyFilters()
  if (activeTab.value === 'errors') {
    errorFilter.value = { model: '', category: '', api_key_id: null, status_code: null }
    applyErrorFilters()
  }
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value.start_date = range.startDate
  filters.value.end_date = range.endDate
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters(false)
}

const handlePageChange = (page: number) => {
  pagination.page = page
  void loadLogs()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadLogs()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  void loadLogs()
}

const handleIpGeoBatchFailed = () => {
  appStore.showError(t('usage.ipGeo.batchFailed'))
}

const getRequestTypeExportText = (log: UsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'cyber') return 'Cyber'
  if (requestType === 'live') return 'Live'
  if (requestType === 'ws_v2') return 'WS'
  if (requestType === 'stream') return 'Stream'
  if (requestType === 'sync') return 'Sync'
  return 'Unknown'
}

const getDisplayBillingMode = (
  row: Pick<UsageLog, 'billing_mode' | 'image_count'> | null | undefined
): string | null | undefined => resolveDisplayBillingMode(row)

const escapeCSVValue = (value: unknown): string => {
  if (value == null) return ''
  const str = String(value)
  const escaped = str.replace(/"/g, '""')
  if (str === '-') return str
  if (/^[=+\-@\t\r]/.test(str)) return `"\'${escaped}"`
  if (/[,"\n\r]/.test(str)) return `"${escaped}"`
  return str
}

const exportToCSV = async () => {
  if (pagination.total === 0) {
    appStore.showWarning(t('usage.noDataToExport'))
    return
  }
  exporting.value = true
  appStore.showInfo(t('usage.preparingExport'))
  try {
    const allLogs: UsageLog[] = []
    const pageSize = 100
    const exportParams = buildUsageListParams(1, pageSize)
    const totalPages = Math.ceil(pagination.total / pageSize)
    for (let page = 1; page <= totalPages; page++) {
      const response = await usageAPI.query({ ...exportParams, page })
      allLogs.push(...response.items)
    }
    if (allLogs.length === 0) {
      appStore.showWarning(t('usage.noDataToExport'))
      return
    }
    const headers = [
      'Time',
      'API Key Name',
      'Model',
      ...(includeUpstreamResponseModel.value ? ['Upstream Response Model'] : []),
      'Reasoning Effort',
      'Inbound Endpoint',
      'IP Address',
      'Type',
      'Billing Mode',
      'Input Tokens',
      'Output Tokens',
      'Cache Read Tokens',
      'Cache Creation Tokens',
      ...(includeCacheRate.value ? ['Cache Rate'] : []),
      'Rate Multiplier',
      'Billed Cost',
      'Original Cost',
      'First Token (ms)',
      'Duration (ms)',
    ]
    const rows = allLogs.map((log) => [
      log.created_at,
      log.api_key?.name || '',
      log.model,
      ...(includeUpstreamResponseModel.value ? [log.upstream_response_model?.trim() || ''] : []),
      formatReasoningEffort(log.reasoning_effort),
      log.inbound_endpoint || '',
      log.ip_address || '',
      getRequestTypeExportText(log),
      getBillingModeLabel(getDisplayBillingMode(log), t),
      log.input_tokens,
      log.output_tokens,
      log.cache_read_tokens,
      log.cache_creation_tokens,
      ...(includeCacheRate.value ? [formatUsageCacheRate(log) ?? ''] : []),
      log.rate_multiplier,
      log.actual_cost.toFixed(8),
      log.total_cost.toFixed(8),
      log.first_token_ms ?? '',
      log.duration_ms ?? '',
    ].map(escapeCSVValue))
    const csvContent = [
      headers.map(escapeCSVValue).join(','),
      ...rows.map((row) => row.join(',')),
    ].join('\n')
    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = exportParams.period === 'all' ? 'usage_all.csv' : `usage_${exportParams.start_date}_to_${exportParams.end_date}.csv`
    link.click()
    window.URL.revokeObjectURL(url)
    appStore.showSuccess(t('usage.exportSuccess'))
  } catch (error) {
    console.error('CSV Export failed:', error)
    appStore.showError(t('usage.exportFailed'))
  } finally {
    exporting.value = false
  }
}

const ALWAYS_VISIBLE = ['created_at']
const DEFAULT_HIDDEN_COLUMNS = ['ip_address', 'user_agent', 'tps']
const HIDDEN_COLUMNS_KEY = 'user-usage-hidden-columns'

const allColumns = computed<Column[]>(() => [
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'tps', label: t('usage.latencyTps'), sortable: false },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'ip_address', label: 'IP', sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'input_tokens', label: t('usage.in'), sortable: false },
  { key: 'output_tokens', label: t('usage.out'), sortable: false },
  { key: 'cache_read_tokens', label: t('usage.cacheHit'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
])

const hiddenColumns = reactive<Set<string>>(new Set())
const toggleableColumns = computed(() => allColumns.value.filter((col) => !ALWAYS_VISIBLE.includes(col.key)))
const visibleColumns = computed(() =>
  allColumns.value.filter((col) => ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key))
)
const isColumnVisible = (key: string) => !hiddenColumns.has(key)
const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) hiddenColumns.delete(key)
  else hiddenColumns.add(key)
  localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
}
const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    const values = saved ? JSON.parse(saved) as string[] : DEFAULT_HIDDEN_COLUMNS
    values.forEach((key) => hiddenColumns.add(key))
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
  }
}

// 错误请求 tab 独立列设置(机制同用量列设置,存储互不影响)
const ERR_ALWAYS_VISIBLE = ['status', 'created_at']
const ERR_DEFAULT_HIDDEN_COLUMNS = ['user_agent']
const ERR_HIDDEN_COLUMNS_KEY = 'user-usage-error-hidden-columns'

// key 须与 UserErrorRequestsTable 的 allColumns 一致
const errAllColumns = computed<Column[]>(() => [
  { key: 'key_name', label: t('usage.errors.keyName') },
  { key: 'model', label: t('usage.errors.model') },
  { key: 'endpoint', label: t('usage.errors.endpoint') },
  { key: 'client_ip', label: 'IP' },
  { key: 'group', label: t('admin.usage.group') },
  { key: 'type', label: t('usage.type') },
  { key: 'platform', label: t('usage.errors.platform') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('usage.errors.status') },
  { key: 'message', label: t('usage.errors.message') },
  { key: 'created_at', label: t('usage.errors.time') },
  { key: 'user_agent', label: t('usage.userAgent') },
])

const errHiddenColumns = reactive<Set<string>>(new Set())
const errToggleableColumns = computed(() =>
  errAllColumns.value.filter((col) => !ERR_ALWAYS_VISIBLE.includes(col.key))
)
const errVisibleColumnKeys = computed(() =>
  errAllColumns.value
    .filter((col) => ERR_ALWAYS_VISIBLE.includes(col.key) || !errHiddenColumns.has(col.key))
    .map((col) => col.key)
)
const isErrColumnVisible = (key: string) => !errHiddenColumns.has(key)
const toggleErrColumn = (key: string) => {
  if (errHiddenColumns.has(key)) errHiddenColumns.delete(key)
  else errHiddenColumns.add(key)
  localStorage.setItem(ERR_HIDDEN_COLUMNS_KEY, JSON.stringify([...errHiddenColumns]))
}
const loadSavedErrColumns = () => {
  try {
    const saved = localStorage.getItem(ERR_HIDDEN_COLUMNS_KEY)
    const values = saved ? (JSON.parse(saved) as string[]) : ERR_DEFAULT_HIDDEN_COLUMNS
    values.forEach((key) => errHiddenColumns.add(key))
  } catch {
    ERR_DEFAULT_HIDDEN_COLUMNS.forEach((key) => errHiddenColumns.add(key))
  }
}

// 列设置下拉按当前 tab 分发
const currentToggleableColumns = computed(() =>
  activeTab.value === 'errors' ? errToggleableColumns.value : toggleableColumns.value
)
const isCurrentColumnVisible = (key: string) =>
  activeTab.value === 'errors' ? isErrColumnVisible(key) : isColumnVisible(key)
const toggleCurrentColumn = (key: string) => {
  if (activeTab.value === 'errors') toggleErrColumn(key)
  else toggleColumn(key)
}

const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)
const handleColumnClickOutside = (event: MouseEvent) => {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as HTMLElement)) {
    showColumnDropdown.value = false
  }
}

const loadApiKeys = async () => {
  const firstPage = await keysAPI.list(1, 100)
  const keys = [...firstPage.items]
  for (let page = 2; page <= firstPage.pages && keys.length > 0; page++) {
    const response = await keysAPI.list(page, 100)
    if (response.items.length === 0) break
    keys.push(...response.items)
  }
  return keys
}

const loadFilterOptions = async () => {
  try {
    const [keys, availableGroups] = await Promise.all([
      loadApiKeys(),
      userGroupsAPI.getAvailable(),
    ])
    apiKeys.value = keys
    groups.value = availableGroups
  } catch (error) {
    console.error('Failed to load usage filter options:', error)
  }
}

const resetErrorRows = () => {
  errorPage.value = 1
  if (activeTab.value === 'errors') {
    void loadErrors()
  } else {
    errorRows.value = []
    errorTotal.value = 0
  }
}

const loadErrors = async () => {
  errorLoading.value = true
  try {
    const resp = await usageAPI.listMyErrorRequests({
      page: errorPage.value,
      page_size: errorPageSize.value,
    start_date: startDate.value || undefined,
    end_date: endDate.value || undefined,
      model: (errorFilter.value.model ?? '').trim() || undefined,
      category: errorFilter.value.category || undefined,
      api_key_id: errorFilter.value.api_key_id ?? undefined,
      status_code: errorFilter.value.status_code ?? undefined,
      sort_by: errorSortBy.value,
      sort_order: errorSortOrder.value,
    })
    errorRows.value = resp.items
    errorTotal.value = resp.total
  } catch (error) {
    console.error('[UsageView] loadErrors failed:', error)
    appStore.showError(t('usage.errors.failedToLoad'))
  } finally {
    errorLoading.value = false
  }
}

const onErrorSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errorSortBy.value = sortBy
  errorSortOrder.value = sortOrder
  errorPage.value = 1
  void loadErrors()
}

const onErrorPage = (page: number) => {
  errorPage.value = page
  void loadErrors()
}

const onErrorPageSize = (pageSize: number) => {
  errorPageSize.value = pageSize
  errorPage.value = 1
  void loadErrors()
}

const switchToErrors = () => {
  activeTab.value = 'errors'
  if (errorRows.value.length === 0) void loadErrors()
}

onMounted(() => {
  loadSavedColumns()
  loadSavedErrColumns()
  document.addEventListener('click', handleColumnClickOutside)
  void loadFilterOptions()
  refreshData()
})

onUnmounted(() => {
  abortController?.abort()
  heatmapReqSeq++
  document.removeEventListener('click', handleColumnClickOutside)
})

watch(endpointDistributionSource, () => {
  // Endpoint source switching is handled by the chart component using already loaded stats.
})
</script>

<style scoped>
.personal-usage-layout {
  --usage-surface: var(--console-surface);
  --usage-line: var(--console-line);
  --usage-text: var(--console-text);
  --usage-muted: var(--console-muted);
  --usage-soft: var(--console-raised);
  color: var(--usage-text);
  letter-spacing: 0;
}
.personal-usage-layout :deep(.app-header h1 + p) { display: none; }
.personal-usage { display: flex; flex-direction: column; min-width: 0; gap: 18px; }
.usage-toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
.usage-group-tabs { display: flex; align-items: center; flex-shrink: 1; gap: 3px; max-width: 40%; overflow-x: auto; padding: 3px; border-radius: 7px; background: var(--usage-soft); }
.usage-group-tabs button { display: flex; align-items: center; justify-content: center; flex-shrink: 0; min-width: 34px; height: 32px; padding: 5px 10px; border-radius: 5px; font-size: 13px; color: var(--usage-muted); }
.usage-group-tabs button.selected { background: var(--usage-surface); color: var(--usage-text); font-weight: 600; box-shadow: 0 1px 3px #0000000d; }
.usage-primary-filters { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-end; gap: 8px; min-width: 0; }
.usage-primary-filters > :deep(.relative):not(:has(.date-picker-trigger)) { width: 136px; }
.usage-primary-filters :deep(.select-trigger), .usage-primary-filters :deep(.date-picker-trigger) { min-height: 36px; height: 36px; border-radius: 6px; border-color: var(--usage-line); background: var(--usage-surface); font-size: 13px; color: var(--usage-text); }
.usage-primary-filters :deep(.date-picker-dropdown) { left: auto; right: 0; width: min(360px, calc(100vw - 24px)); min-width: 0; border-radius: 8px; }
.usage-primary-filters :deep(.date-picker-field), .usage-primary-filters :deep(.date-picker-input) { min-width: 0; }
.usage-primary-filters :deep(.date-picker-input) { max-width: 100%; }
.usage-icon-button { display: grid; place-items: center; flex-shrink: 0; width: 32px; height: 32px; border-radius: 5px; color: var(--usage-muted); }
.usage-icon-button:hover, .usage-filter-button:hover, .usage-export-button:hover { background: var(--usage-soft); color: var(--usage-text); }
button:disabled { opacity: .45; cursor: default; }
.usage-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)) auto; align-items: start; gap: 12px; }
.usage-stat { min-width: 0; container-type: inline-size; border: 1px solid var(--usage-line); border-radius: var(--console-radius); padding: 20px; background: var(--usage-surface); box-shadow: var(--console-shadow); }
.usage-stat:first-child { border-top: 2px solid var(--console-accent); padding-top: 19px; }
.usage-stat-label { display: flex; align-items: center; gap: 5px; min-height: 20px; font-size: 13px; line-height: 20px; color: var(--usage-muted); }
.usage-stat-label > span { display: inline-flex; cursor: help; }
.usage-stat-value { margin-top: 12px; font-size: min(30px, calc(160cqi / var(--usage-value-length))); line-height: 36px; font-weight: 700; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.usage-more-metrics { display: flex; align-items: center; justify-content: flex-end; gap: 6px; padding: 17px 0 0 6px; font-size: 12px; white-space: nowrap; color: var(--usage-muted); }
.usage-extra-metrics { display: flex; flex-wrap: wrap; gap: 12px 22px; padding-bottom: 12px; border-bottom: 1px solid var(--usage-line); font-size: 12px; color: var(--usage-muted); }
.usage-extra-metrics strong { margin-left: 6px; font-weight: 550; color: var(--usage-text); }
.usage-history-notice { display: flex; align-items: flex-start; gap: 8px; font-size: 13px; line-height: 20px; color: var(--usage-muted); }
.usage-history-notice :deep(svg) { flex-shrink: 0; margin-top: 2px; }
.usage-records-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 6px 12px; border-bottom: 1px solid var(--usage-line); margin-top: 3px; }
.usage-view-tabs { display: flex; align-items: stretch; gap: 22px; overflow-x: auto; }
.usage-view-tabs button { position: relative; flex-shrink: 0; padding: 10px 0 11px; font-size: 14px; color: var(--usage-muted); }
.usage-view-tabs .selected { font-weight: 600; color: var(--usage-text); }
.usage-view-tabs .selected::after { content: ''; position: absolute; inset: auto 0 0; height: 2px; border-radius: 2px; background: var(--console-accent); }
.usage-record-actions { display: flex; align-items: center; gap: 5px; }
.usage-filter-button, .usage-export-button { display: inline-flex; align-items: center; gap: 5px; height: 32px; padding: 4px 8px; border-radius: 5px; font-size: 12px; color: var(--usage-muted); }
.usage-filter-count { min-width: 16px; border-radius: 3px; background: var(--usage-soft); font-size: 10px; text-align: center; }
.usage-filter-button.selected { color: var(--usage-text); background: var(--usage-soft); }
.usage-column-menu { position: absolute; right: 0; top: 100%; z-index: 40; width: 200px; max-height: 320px; overflow-y: auto; padding: 4px; border: 1px solid var(--usage-line); border-radius: 6px; background: var(--usage-surface); box-shadow: 0 8px 24px #00000014; }
.usage-column-menu button { display: flex; align-items: center; justify-content: space-between; width: 100%; padding: 7px 9px; border-radius: 4px; font-size: 12px; text-align: left; }
.usage-column-menu button:hover { background: var(--usage-soft); }
.usage-advanced-filters { padding: 14px 0; border-bottom: 1px solid var(--usage-line); }
.usage-records { min-width: 0; background: var(--usage-surface); }
.usage-records :deep(.table-wrapper thead.table-header), .usage-records :deep(.table-header .sticky-header-cell), .usage-records :deep(tbody.table-body) { background: var(--usage-surface); }
.usage-records :deep(th) { padding: 12px 9px; font-size: 14px; letter-spacing: 0; text-transform: none; color: var(--usage-muted); }
.usage-records :deep(td) { padding: 12px 9px; font-size: 14px; font-variant-numeric: tabular-nums; }
.usage-records :deep(td .text-sm), .usage-records :deep(td .text-xs) { font-size: 14px; }
.usage-records :deep(tbody tr:hover) { background: var(--usage-soft); }
@media (max-width: 1199px) {
  .usage-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .usage-more-metrics { grid-column: 1 / -1; padding: 0; margin-top: -4px; }
}
@media (max-width: 767px) {
  .usage-primary-filters :deep(.select-trigger), .usage-primary-filters :deep(.date-picker-trigger) { min-height: 44px; height: 44px; }
  .usage-icon-button, .usage-filter-button, .usage-export-button { min-width: 44px; min-height: 44px; }
  .personal-usage { gap: 14px; }
  .usage-group-tabs { max-width: 100%; }
  .usage-group-tabs button { min-width: 44px; height: 44px; }
  .usage-more-metrics { min-height: 44px; align-items: center; }
  .usage-view-tabs button, .usage-column-menu button { min-height: 44px; }
  .usage-primary-filters { position: relative; width: 100%; justify-content: flex-start; }
  .usage-primary-filters > :deep(.relative):not(:has(.date-picker-trigger)) { flex: 1; width: auto; min-width: 120px; }
  .usage-primary-filters > :deep(.relative):has(.date-picker-trigger) { position: static; flex: 1 0 calc(100% - 100px); }
  .usage-primary-filters :deep(.date-picker-dropdown) { top: 100%; left: 0; right: auto; }
  .usage-primary-filters :deep(.date-picker-trigger) { width: 100%; }
  .usage-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .usage-stat { padding: 12px 14px; }
  .usage-stat:first-child { padding-top: 11px; }
  .usage-stat-value { font-size: min(23px, calc(160cqi / var(--usage-value-length))); line-height: 30px; }
  .usage-view-tabs { gap: 18px; width: 100%; }
  .usage-record-actions { margin-left: auto; padding-bottom: 5px; }
}
@media (prefers-reduced-motion: reduce) { .animate-spin { animation: none; } }
</style>
