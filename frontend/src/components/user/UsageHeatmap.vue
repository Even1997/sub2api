<template>
  <section class="usage-heatmap" :aria-label="t('usage.personal.heatmap')" :aria-busy="loading">
    <div class="heatmap-heading">
      <div>
        <h2>{{ t('usage.personal.heatmap') }}</h2>
        <p>{{ t('usage.personal.heatmapPeriod') }}</p>
      </div>
      <div class="heatmap-metrics" role="group" :aria-label="t('usage.personal.metric')">
        <button
          v-for="option in metricOptions"
          :key="option.value"
          type="button"
          :aria-pressed="metric === option.value"
          :class="{ selected: metric === option.value }"
          @click="metric = option.value"
        >{{ option.label }}</button>
        <button type="button" class="heatmap-details" @click="$emit('details')">{{ t('usage.hourly.details') }}</button>
      </div>
    </div>
    <p v-if="coverage" class="heatmap-coverage" role="status">{{ coverage.backfill_pending ? t('usage.hourly.backfill') : t('usage.hourly.coverageNote') }}</p>
    <div class="heatmap-summary">
      <span class="heatmap-date">{{ startDate }} ~ {{ endDate }}</span>
      <span v-if="(!loading || data.length > 0) && !error">{{ formatCompactNumber(totals.tokens) }} Tokens · {{ totals.requests.toLocaleString() }} {{ t('usage.personal.requests') }} · ${{ totals.cost.toFixed(2) }}</span>
      <span v-else-if="loading">{{ t('common.loading') }}</span>
      <div class="heatmap-navigation">
        <button type="button" :title="t('usage.personal.previousPeriod')" :aria-label="t('usage.personal.previousPeriod')" :disabled="loading" @click="$emit('navigate', -1)"><Icon name="chevronLeft" size="sm" /></button>
        <button type="button" :title="t('usage.personal.nextPeriod')" :aria-label="t('usage.personal.nextPeriod')" :disabled="loading || !canGoNext" @click="$emit('navigate', 1)"><Icon name="chevronRight" size="sm" /></button>
      </div>
    </div>
    <div v-if="error" class="heatmap-error" role="alert">
      <span>{{ t('usage.failedToLoad') }}</span>
      <button type="button" @click="$emit('retry')"><Icon name="refresh" size="sm" />{{ t('common.tryAgain') }}</button>
    </div>
    <template v-else>
      <div class="heatmap-scroll" tabindex="0" :aria-label="t('usage.personal.heatmap')">
        <div class="heatmap-calendar" :class="{ 'is-loading': loading && !data.length }">
          <div class="heatmap-months" aria-hidden="true">
            <span v-for="month in months" :key="month.column" :style="{ gridColumn: month.column }">{{ month.label }}</span>
          </div>
          <div class="heatmap-weekdays" aria-hidden="true">
            <span v-for="day in weekdays" :key="day.row" :style="{ gridRow: day.row }">{{ day.label }}</span>
          </div>
          <div class="heatmap-days">
            <button
              v-for="day in days"
              :key="day.date"
              type="button"
              class="heatmap-day"
              :class="[`level-${day.level}`, { 'is-future': day.future, 'is-selected': day.date === selectedDate, 'is-incomplete': isIncomplete(day.date) }]"
              :disabled="loading || day.future"
              :data-date="day.date"
              :aria-label="day.label"
              :aria-pressed="day.date === selectedDate"
              :aria-describedby="tooltip?.date === day.date ? 'usage-heatmap-day-tooltip' : undefined"
              :tabindex="day.date === activeDate ? 0 : -1"
              @mouseenter="showDayTooltip($event, day.date)"
              @mouseleave="tooltip = null"
              @focus="focusedDate = day.date; showDayTooltip($event, day.date)"
              @blur="tooltip = null"
              @keydown="onDayKeydown"
              @keydown.esc="tooltip = null"
              @click="$emit('select', day.date)"
            />
          </div>
        </div>
      </div>
      <div v-if="tooltip" id="usage-heatmap-day-tooltip" class="heatmap-day-tooltip" role="tooltip" :style="{ left: `${tooltip.x}px`, top: `${tooltip.y}px` }">
        <strong>{{ tooltip.date }}</strong>
        <p v-if="isIncomplete(tooltip.date)">{{ t('usage.hourly.unknown') }}</p>
        <dl v-if="tooltipPoint || !isIncomplete(tooltip.date)">
          <div><dt>{{ t('usage.totalCost') }}</dt><dd>${{ (tooltipPoint?.actual_cost ?? 0).toFixed(4) }}</dd></div>
          <div><dt>{{ t('usage.totalTokens') }}</dt><dd>{{ (tooltipPoint?.total_tokens ?? 0).toLocaleString(locale) }}</dd></div>
        </dl>
      </div>
      <div class="heatmap-footer">
        <span v-if="!loading && totals.requests === 0 && !days.some(day => isIncomplete(day.date))">{{ t('usage.personal.noActivity') }}</span>
        <div class="heatmap-legend" :aria-label="t('usage.personal.intensity')">
          <span>{{ t('usage.personal.less') }}</span>
          <span v-for="level in 5" :key="level" class="heatmap-swatch" :class="`level-${level - 1}`" aria-hidden="true" />
          <span>{{ t('usage.personal.more') }}</span>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatCompactNumber } from '@/utils/format'
import type { HourlyUsageReport } from '@/api/usage'
import type { TrendDataPoint } from '@/types'

const props = withDefaults(defineProps<{
  data: TrendDataPoint[]
  startDate: string
  endDate: string
  loading?: boolean
  error?: boolean
  canGoNext?: boolean
  selectedDate?: string
  coverage?: HourlyUsageReport | null
}>(), { loading: false, error: false, canGoNext: false, selectedDate: '' })
defineEmits<{ select: [date: string]; navigate: [direction: number]; retry: []; details: [] }>()
const { t, locale } = useI18n()
const isIncomplete = (date: string) => !!props.coverage && date <= props.endDate && (props.coverage.backfill_pending || new Date(`${date}T00:00:00+08:00`) < new Date(props.coverage.complete_from) || props.coverage.missing_months.includes(date.slice(0, 7)))
const focusedDate = ref('')
const activeDate = computed(() => {
  const date = focusedDate.value || props.selectedDate
  return date >= props.startDate && date <= props.endDate ? date : props.endDate
})
const onDayKeydown = (event: KeyboardEvent) => {
  const offsets: Record<string, number> = { ArrowUp: -1, ArrowDown: 1, ArrowLeft: -7, ArrowRight: 7 }
  const offset = offsets[event.key]
  if (offset == null) return
  event.preventDefault()
  const target = event.currentTarget as HTMLButtonElement
  const index = calendar.value.findIndex((day) => day.date === target.dataset.date) + offset
  const next = target.parentElement?.children[index] as HTMLButtonElement | undefined
  if (next && !next.disabled) next.focus()
}
const metric = ref<'total_tokens' | 'requests' | 'actual_cost'>('total_tokens')
const metricOptions = computed(() => [
  { value: 'total_tokens' as const, label: 'Tokens' },
  { value: 'requests' as const, label: t('usage.personal.requests') },
  { value: 'actual_cost' as const, label: t('usage.cost') },
])
const dataByDate = computed(() => new Map(props.data.map((point) => [point.date, point])))
const tooltip = ref<{ date: string; x: number; y: number } | null>(null)
const clearTooltip = () => { tooltip.value = null }
onMounted(() => {
  window.addEventListener('scroll', clearTooltip, { capture: true, passive: true })
  window.addEventListener('resize', clearTooltip)
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', clearTooltip, true)
  window.removeEventListener('resize', clearTooltip)
})
const tooltipPoint = computed(() => tooltip.value ? dataByDate.value.get(tooltip.value.date) : undefined)
const showDayTooltip = (event: MouseEvent | FocusEvent, date: string) => {
  if (props.loading || date > props.endDate) return
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  tooltip.value = {
    date,
    x: Math.max(8, Math.min(rect.left + rect.width / 2 - 104, window.innerWidth - 216)),
    y: rect.bottom + 104 > window.innerHeight ? rect.top - 104 : rect.bottom + 8,
  }
}
watch(() => [props.startDate, props.endDate, props.loading, props.error], () => { tooltip.value = null })
const totals = computed(() => props.data.reduce((sum, point) => {
  if (point.date >= props.startDate && point.date <= props.endDate) {
    sum.tokens += point.total_tokens
    sum.requests += point.requests
    sum.cost += point.actual_cost
  }
  return sum
}, { tokens: 0, requests: 0, cost: 0 }))
const weekdays = computed(() => [0, 2, 4, 6].map((index) => ({
  row: index + 1,
  label: new Intl.DateTimeFormat(locale.value, { weekday: 'short', timeZone: 'UTC' }).format(new Date(Date.UTC(2026, 0, 5 + index))),
})))
const calendar = computed(() => {
  const date = new Date(`${props.startDate}T00:00:00Z`)
  return Array.from({ length: 53 * 7 }, (_, index) => {
    const value = new Date(date)
    value.setUTCDate(date.getUTCDate() + index)
    return { date: value.toISOString().slice(0, 10), value }
  })
})
const months = computed(() => calendar.value.flatMap((day, index) => {
  if (index !== 0 && day.value.getUTCDate() !== 1) return []
  const column = Math.floor(index / 7) + 1
  // The first partial month can share a week with the next month's label.
  if (index === 0 && calendar.value[6].value.getUTCMonth() !== day.value.getUTCMonth()) return []
  return [{ column, label: new Intl.DateTimeFormat(locale.value, { month: 'short', timeZone: 'UTC' }).format(day.value) }]
}))
const days = computed(() => {
  const max = Math.max(0, ...props.data.filter((point) => point.date >= props.startDate && point.date <= props.endDate).map((point) => point[metric.value]))
  return calendar.value.map((day) => {
    const point = dataByDate.value.get(day.date)
    const value = point?.[metric.value] ?? 0
    const display = metric.value === 'actual_cost' ? `$${value.toFixed(4)}` : value.toLocaleString()
    const label = metricOptions.value.find((option) => option.value === metric.value)!.label
    return {
      date: day.date,
      future: day.date > props.endDate,
      level: value > 0 && max > 0 ? Math.min(4, Math.ceil(value / max * 4)) : 0,
      label: isIncomplete(day.date) && !point ? `${day.date}: ${t('usage.hourly.unknown')}` : `${day.date}: ${isIncomplete(day.date) ? t('usage.hourly.unknown') + '; ' : ''}${display} ${label}; ${t('usage.totalCost')}: $${(point?.actual_cost ?? 0).toFixed(4)}; ${t('usage.totalTokens')}: ${(point?.total_tokens ?? 0).toLocaleString(locale.value)}`,
    }
  })
})
</script>

<style scoped>
.heatmap-metrics .heatmap-details { margin-left: 4px; border-left: 1px solid var(--usage-line, #e1e2e8); border-radius: 0; color: var(--usage-text, #17171d); }
.heatmap-day.is-incomplete { background-image: repeating-linear-gradient(135deg, transparent, transparent 2px, #71717a66 2px, #71717a66 3px); }
.heatmap-coverage { font-size: 11px; margin-top: 10px; }
.usage-heatmap { border: 1px solid var(--usage-line, #e1e2e8); border-radius: var(--console-radius, 12px); padding: 18px 20px 16px; background: var(--usage-surface, #fff); color: var(--usage-text, #17171d); box-shadow: var(--console-shadow); }
.heatmap-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
h2 { margin: 0; font-size: 16px; font-weight: 650; line-height: 22px; }
p { margin-top: 3px; font-size: 13px; line-height: 20px; color: var(--usage-muted, #666672); }
.heatmap-metrics { display: flex; flex-shrink: 0; padding: 3px; border-radius: 7px; background: var(--usage-soft, #f0f0f4); }
.heatmap-metrics button { padding: 4px 12px; border-radius: 5px; font-size: 12px; line-height: 20px; color: var(--usage-muted, #666672); }
.heatmap-metrics .selected { background: var(--usage-surface, #fff); color: var(--usage-text, #17171d); box-shadow: 0 1px 3px #0000000d; }
.heatmap-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 12px; margin: 14px 0 10px; font-size: 13px; color: var(--usage-muted, #666672); font-variant-numeric: tabular-nums; }
.heatmap-date { font-weight: 550; color: var(--usage-text, #17171d); }
.heatmap-navigation { margin-left: auto; display: flex; gap: 2px; }
.heatmap-navigation button { display: grid; place-items: center; width: 26px; height: 26px; border: 1px solid var(--usage-line, #e1e2e8); border-radius: 5px; }
button:disabled { opacity: .35; cursor: default; }
.heatmap-scroll { overflow-x: auto; padding: 2px 2px 5px; }
.heatmap-calendar { display: grid; grid-template-columns: 32px minmax(0, 1fr); grid-template-rows: 18px auto; column-gap: 8px; row-gap: 6px; min-width: 760px; }
.heatmap-months { grid-column: 2; display: grid; grid-template-columns: repeat(53, minmax(0, 1fr)); gap: 4px; font-size: 11px; color: var(--usage-muted, #666672); }
.heatmap-months span { white-space: nowrap; }
.heatmap-weekdays { grid-row: 2; display: grid; grid-template-rows: repeat(7, minmax(0, 1fr)); gap: 4px; font-size: 11px; line-height: 12px; color: var(--usage-muted, #666672); }
.heatmap-weekdays span { align-self: center; }
.heatmap-days { grid-column: 2; grid-row: 2; display: grid; grid-auto-flow: column; grid-template-columns: repeat(53, minmax(0, 1fr)); grid-template-rows: repeat(7, auto); gap: 4px; }
.heatmap-day { display: block; width: 100%; min-width: 0; aspect-ratio: 1; border-radius: 3px; }
.heatmap-day:disabled { opacity: 1; }
.heatmap-day:focus-visible { outline: 2px solid var(--usage-text, #17171d); outline-offset: 1px; }
.heatmap-day:hover:not(:disabled) { outline: 1px solid var(--usage-muted, #666672); outline-offset: 1px; }
.heatmap-day.is-selected { outline: 2px solid var(--usage-text, #17171d); outline-offset: 1px; }
.heatmap-day.is-future { visibility: hidden; }
.heatmap-day-tooltip { position: fixed; z-index: 60; width: 208px; padding: 10px 12px; border: 1px solid var(--usage-line, #e1e2e8); border-radius: 6px; background: var(--usage-surface, #fff); color: var(--usage-text, #17171d); box-shadow: 0 4px 16px #0000001a; font-size: 12px; line-height: 18px; pointer-events: none; }
.heatmap-day-tooltip strong { display: block; margin-bottom: 6px; font-weight: 600; }
.heatmap-day-tooltip dl > div { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.heatmap-day-tooltip dt { color: var(--usage-muted, #666672); }
.heatmap-day-tooltip dd { font-variant-numeric: tabular-nums; }
.level-0 { background: var(--usage-soft, #eeeef2); }
.level-1 { background: #c7f0d7; }
.level-2 { background: #81dba5; }
.level-3 { background: #36ba72; }
.level-4 { background: #16864e; }
.heatmap-footer { display: flex; align-items: center; gap: 12px; min-height: 22px; margin-top: 9px; font-size: 11px; color: var(--usage-muted, #666672); }
.heatmap-legend { display: flex; align-items: center; gap: 5px; margin-left: auto; }
.heatmap-swatch { width: 13px; height: 13px; border-radius: 3px; }
.heatmap-error { display: flex; justify-content: center; align-items: center; gap: 12px; min-height: 150px; font-size: 13px; color: var(--usage-muted, #666672); }
.heatmap-error button { display: inline-flex; align-items: center; gap: 5px; text-decoration: underline; }
.is-loading { opacity: .4; }
@media (max-width: 640px) {
  .usage-heatmap { padding: 14px 12px; }
  .heatmap-heading { flex-wrap: wrap; }
  .heatmap-metrics button { min-width: 44px; min-height: 44px; padding-inline: 10px; }
  .heatmap-navigation button { width: 44px; height: 44px; }
  .heatmap-summary { font-size: 11px; }
}
</style>
