<template>
  <BaseDialog :show="show" :title="t('usage.hourly.title')" placement="right" width="extra-wide" @close="$emit('close')">
    <div class="hourly-usage">
      <p class="hourly-caption">{{ t('usage.hourly.description') }}</p>
      <div class="hourly-filters">
        <label>{{ t('usage.hourly.date') }}<input v-model="date" type="date" :max="today" class="input" /></label>
        <label>{{ t('usage.model') }}<select v-model="model" class="input"><option value="">{{ t('admin.usage.allModels') }}</option><option v-for="name in models" :key="name" :value="name">{{ name }}</option></select></label>
        <button class="btn btn-secondary" type="button" :disabled="loading || !date" @click="load">{{ t('common.refresh') }}</button>
      </div>
      <p v-if="error" role="alert" class="hourly-notice">{{ t('usage.failedToLoad') }} <button type="button" class="underline" @click="load">{{ t('common.tryAgain') }}</button></p>
      <p v-else-if="loading" role="status" class="hourly-caption">{{ t('common.loading') }}</p>
      <template v-else-if="report">
        <p v-if="report.backfill_pending" role="status" class="hourly-notice">{{ t('usage.hourly.backfill') }}</p>
        <p v-else-if="incomplete" role="status" class="hourly-notice">{{ t('usage.hourly.incomplete') }}</p>
        <div class="hourly-totals">
          <div><span>{{ t(incomplete || report.backfill_pending ? 'usage.hourly.retainedUsage' : 'usage.hourly.totalUsage') }}</span><strong>{{ quantity(totals[primaryUnit]) }} <small>{{ t(`bills.units.${primaryUnit}`) }}</small></strong><p>{{ quantities(totals, primaryUnit) }}</p></div>
          <div><span>{{ t(incomplete || report.backfill_pending ? 'usage.hourly.retainedCost' : 'usage.hourly.totalCost') }}</span><strong :title="totals.cost">${{ money(totals.cost) }}</strong><p>{{ t('usage.hourly.beforeRebate') }}</p></div>
        </div>
        <p class="hourly-caption">{{ t('usage.hourly.billingBreakdown', { balance: money(balance), subscription: money(subscription) }) }}</p>
        <label class="hourly-exact"><input v-model="exact" type="checkbox" /> {{ t('bills.exact') }}</label>
        <p class="hourly-scroll-hint">{{ t('usage.hourly.scrollHint') }}</p>
        <div class="hourly-table-wrap" tabindex="0" :aria-label="t('usage.hourly.title')">
          <table>
            <thead><tr><th>{{ t('usage.hourly.hour') }}</th><th>{{ t('usage.model') }}</th><th>{{ t('usage.hourly.usage') }}</th><th class="money">{{ t('usage.cost') }}</th></tr></thead>
            <tbody>
              <tr v-for="row in items" :key="`${row.hour}:${row.model}:${row.billing_type}`">
                <td class="hourly-time">{{ hour(row.hour) }}<span v-if="currentHour(row.hour)" class="hourly-live">{{ t('usage.hourly.inProgress') }}</span></td>
                <td class="hourly-model">{{ row.model }}<span v-if="row.billing_type === 1" class="hourly-kind">{{ t('usage.hourly.subscription') }}</span></td>
                <td class="hourly-quantity">{{ quantities(row) }}</td>
                <td class="money" :title="row.cost">${{ money(row.cost) }}</td>
              </tr>
              <tr v-if="!items.length"><td colspan="4" class="hourly-empty">{{ incomplete || report.backfill_pending ? t('usage.hourly.unavailable') : t('usage.hourly.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <p class="hourly-caption">{{ t('usage.hourly.updatedAt', { at: new Date(report.generated_at).toLocaleTimeString(locale, { timeZone: 'Asia/Shanghai', hour12: false }) }) }}</p>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { usageAPI, type HourlyUsageReport, type HourlyUsageValues, type TrendParams } from '@/api/usage'

const props = defineProps<{ show: boolean; initialDate: string; filters: TrendParams }>()
defineEmits<{ close: [] }>()
const { t, locale } = useI18n()
const date = ref('')
const model = ref('')
const report = ref<HourlyUsageReport | null>(null)
const loading = ref(false)
const error = ref(false)
const exact = ref(false)
let request: AbortController | undefined
const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date())
const models = computed(() => [...new Set([...(report.value?.items.map(row => row.model) ?? []), ...(model.value ? [model.value] : [])])].sort())
const items = computed(() => (report.value?.items ?? []).filter(row => !model.value || row.model === model.value))
// Ledger amounts have 10 fractional digits; BigInt keeps sums exact.
const units = (value: string) => {
  const [whole, fraction = ''] = value.replace(/^-/, '').split('.')
  return (BigInt(whole || '0') * 10000000000n + BigInt(fraction.padEnd(10, '0'))) * (value.startsWith('-') ? -1n : 1n)
}
const fromUnits = (value: bigint) => `${value < 0n ? '-' : ''}${(value < 0n ? -value : value) / 10000000000n}.${((value < 0n ? -value : value) % 10000000000n).toString().padStart(10, '0')}`
const totals = computed(() => {
  const result: HourlyUsageValues = { requests: '0', tokens: '0', images: '0', videos: '0', video_seconds: '0', cost: '0' }
  for (const row of items.value) for (const key of Object.keys(result) as (keyof HourlyUsageValues)[]) result[key] = key === 'cost' ? fromUnits(units(result[key]) + units(row[key])) : (BigInt(result[key]) + BigInt(row[key])).toString()
  return result
})
const costByKind = (kind: number) => items.value.filter(row => row.billing_type === kind).reduce((sum, row) => sum + units(row.cost), 0n)
const primaryUnit = computed(() => (['tokens', 'images', 'video_seconds', 'videos', 'requests'] as const).find(key => BigInt(totals.value[key]) !== 0n) || 'tokens')
const balance = computed(() => fromUnits(costByKind(0)))
const subscription = computed(() => fromUnits(costByKind(1)))
const incomplete = computed(() => !!report.value && (new Date(`${date.value}T00:00:00+08:00`) < new Date(report.value.complete_from) || report.value.missing_months.includes(date.value.slice(0, 7))))
const quantity = (value: string) => BigInt(value).toLocaleString(locale.value)
const money = (value: string) => {
  if (exact.value) return value.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1')
  const amount = units(value)
  const absolute = amount < 0n ? -amount : amount
  if (absolute > 0n && absolute < 1000000n) return amount < 0n ? '>−0.0001' : '<0.0001'
  const rounded = (absolute + 500000n) / 1000000n
  return `${amount < 0n ? '−' : ''}${rounded / 10000n}.${(rounded % 10000n).toString().padStart(4, '0')}`
}
const quantities = (row: HourlyUsageValues, exclude?: keyof HourlyUsageValues) => (['requests', 'tokens', 'images', 'video_seconds', 'videos'] as const).filter(key => key !== exclude && BigInt(row[key]) !== 0n).map(key => `${quantity(row[key])} ${t(`bills.units.${key}`)}`).join(' · ') || '—'
const hour = (value: string) => new Date(value).toLocaleTimeString(locale.value, { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', hour12: false })
const currentHour = (value: string) => Math.floor(new Date(value).getTime() / 3600000) === Math.floor(new Date(report.value!.generated_at).getTime() / 3600000)
async function load() {
  request?.abort()
  if (!props.show || !date.value || date.value > today) { report.value = null; loading.value = false; return }
  const controller = new AbortController()
  request = controller
  loading.value = true
  error.value = false
  report.value = null
  try {
    const response = await usageAPI.getHourlySummary({ ...props.filters, model: undefined, start_date: date.value, end_date: date.value, period: undefined, granularity: 'hour' }, controller.signal)
    if (!controller.signal.aborted) report.value = response
  } catch {
    if (!controller.signal.aborted) error.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}
watch(date, () => { if (props.show) void load() })
watch(() => props.show, show => {
  if (show) {
    model.value = props.filters.model || ''
    const target = props.initialDate || today
    if (date.value === target) void load()
    else date.value = target
  } else request?.abort()
}, { immediate: true })
onBeforeUnmount(() => request?.abort())
</script>

<style scoped>
.hourly-usage { color: var(--usage-text, #18181b); }
.hourly-caption { color: #71717a; font-size: 12px; line-height: 1.7; margin-bottom: 16px; }
.hourly-filters { display: flex; align-items: end; gap: 12px; margin: 20px 0; }
.hourly-filters label { display: grid; gap: 6px; font-size: 12px; flex: 1; min-width: 0; }
.hourly-filters .input { width: 100%; }
.hourly-totals { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; padding: 20px 0; border-top: 1px solid #e4e4e7; }
.hourly-totals span { font-size: 12px; color: #71717a; }
.hourly-totals strong { display: block; margin: 6px 0; font-size: 25px; font-weight: 600; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
.hourly-totals small { font-size: 13px; font-weight: 400; }
.hourly-totals p { font-size: 12px; color: #71717a; }
.hourly-notice { padding: 12px; border-radius: 8px; background: #fffbeb; color: #92400e; font-size: 13px; margin: 12px 0; }
.hourly-scroll-hint { display: none; margin-bottom: 8px; font-size: 11px; color: #71717a; }
.hourly-exact { display: flex; align-items: center; gap: 6px; margin: 0 0 12px; font-size: 12px; color: #71717a; }
.hourly-table-wrap { overflow-x: auto; margin-bottom: 14px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th { text-align: left; font-weight: 500; color: #71717a; background: #fafafa; }
th, td { padding: 13px 10px; border-bottom: 1px solid #e4e4e7; vertical-align: top; }
.hourly-time { white-space: nowrap; font-variant-numeric: tabular-nums; }
.hourly-model { overflow-wrap: anywhere; max-width: 220px; min-width: 120px; }
.hourly-quantity { min-width: 130px; max-width: 230px; font-size: 12px; line-height: 1.8; }
.money { text-align: right; white-space: nowrap; font-variant-numeric: tabular-nums; }
.hourly-live, .hourly-kind { display: block; font-size: 10px; margin-top: 4px; color: #71717a; }
.hourly-live { color: #047857; }
.hourly-empty { text-align: center; padding: 44px 12px; color: #71717a; }
.dark .hourly-usage { color: #e4e4e7; }
.dark .hourly-caption, .dark .hourly-totals span, .dark .hourly-totals p, .dark .hourly-exact, .dark .hourly-scroll-hint, .dark .hourly-kind { color: #a1a1aa; }
.dark th { background: #27272a; color: #a1a1aa; }
.dark th, .dark td, .dark .hourly-totals { border-color: #3f3f46; }
.dark .hourly-notice { background: #422006; color: #fde68a; }
.dark .hourly-live { color: #6ee7b7; }
@media (max-width: 540px) { .hourly-scroll-hint { display: block; } .hourly-filters { flex-wrap: wrap; } .hourly-filters label { flex-basis: 40%; } .hourly-totals { gap: 16px; } .hourly-totals strong { font-size: 21px; } }
</style>
