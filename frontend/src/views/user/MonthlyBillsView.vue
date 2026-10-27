<template>
  <AppLayout>
    <div class="bill-page">
      <div class="bill-toolbar">
        <h1>{{ t('bills.title') }}</h1>
        <Select v-model="month" class="bill-month" :options="monthOptions" :disabled="busy" :aria-label="t('bills.month')" data-test="bill-month" />
        <span class="bill-manual">{{ t('bills.manual') }}</span>
        <button class="btn btn-primary" :disabled="busy || !validMonth || !!loadError" data-test="bill-generate" @click="versions.length ? confirmRegenerate = true : generate()">{{ generating ? t('bills.generating') : versions.length ? t('bills.regenerate') : t('bills.generate') }}</button>
        <button class="btn btn-secondary" :disabled="!bill || busy" data-test="bill-print" @click="printBill">{{ t('bills.print') }}</button>
      </div>
      <div v-if="error || loadError" class="bill-error" role="alert">{{ error || loadError }} <button v-if="loadError" class="btn btn-secondary" @click="loadMonth">{{ t('bills.retry') }}</button></div>
      <div v-if="loading" class="bill-empty" role="status">{{ t('bills.loading') }}</div>
      <div v-else-if="!bill && !loadError" class="bill-empty">
        <p class="bill-eyebrow">MONTHLY STATEMENT</p><h2>{{ t('bills.empty') }}</h2><p>{{ t('bills.emptyHint') }}</p>
      </div>
      <template v-else-if="bill">
        <div class="bill-options">
          <label class="bill-version-field">{{ t('bills.version') }} <Select v-model="version" class="bill-version" :options="versionOptions" :disabled="busy" :aria-label="t('bills.version')" data-test="bill-version" @change="loadVersion" /></label>
          <label><input v-model="exact" type="checkbox" /> {{ t('bills.exact') }}</label>
        </div>
        <article class="bill-paper" data-test="bill-paper">
          <header class="bill-heading">
            <div class="bill-brand"><img v-if="app.siteLogo" :src="app.siteLogo" alt="" />{{ app.siteName }}</div>
            <div><p class="bill-eyebrow">MONTHLY STATEMENT</p><h2>{{ t('bills.statement') }}</h2><span v-if="bill.partial" class="bill-partial">{{ t('bills.partial') }}</span></div>
          </header>
          <section class="bill-meta">
            <div><p class="bill-muted">{{ t('bills.account') }}</p><strong>#{{ bill.user_id }}</strong><p class="bill-email">{{ bill.email }}</p></div>
            <dl><dt>{{ t('bills.reference') }}</dt><dd>{{ bill.month.replace('-', '') }}-{{ bill.user_id }}-v{{ bill.version }}</dd><dt>{{ t('bills.period') }}</dt><dd>{{ bill.month }}<span v-if="bill.partial"> · {{ dateTime(bill.end) }}</span></dd><dt>{{ t('bills.generated') }}</dt><dd>{{ dateTime(bill.generated_at) }}</dd><dt>{{ t('bills.timezone') }}</dt><dd>{{ bill.timezone }}</dd></dl>
          </section>
          <section class="bill-hero">
            <div><h3>{{ bill.net.startsWith('-') ? t('bills.inflow') : t('bills.actual') }}</h3><p class="bill-amount" data-test="bill-amount" :title="bill.cny">¥{{ money(bill.cny) }}</p><p class="bill-muted">{{ t('bills.deducted') }}</p></div>
            <div class="bill-ratio"><strong>{{ t('bills.ratio') }}</strong><p class="bill-muted">{{ t('bills.currencyNote') }}</p></div>
          </section>
          <section class="bill-section">
            <h3><span>01</span>{{ t('bills.metering') }}</h3>
            <BillModelTable :models="bill.models" :exact="exact" />
            <div class="bill-settlement">
              <p class="bill-muted">{{ t('bills.rebateHint') }}</p>
              <dl><dt>{{ t('bills.consumption') }}</dt><dd>${{ money(bill.consumption) }}</dd><dt>{{ t('bills.rebate') }}</dt><dd class="bill-rebate">−${{ money(bill.rebate) }}</dd><dt class="bill-net">{{ t('bills.net') }}</dt><dd class="bill-net">${{ money(bill.net) }}</dd><dt class="bill-total">{{ t('bills.settlement') }}</dt><dd class="bill-total">¥{{ money(bill.cny) }}</dd></dl>
            </div>
          </section>
          <section v-if="bill.subscription_models.length" class="bill-section">
            <h3>{{ t('bills.subscription') }}</h3><p class="bill-muted">{{ t('bills.subscriptionHint') }}</p>
            <BillModelTable :models="bill.subscription_models" :exact="exact" />
            <p class="bill-subtotal">{{ t('bills.total') }} ${{ money(bill.subscription) }}</p>
          </section>
          <section class="bill-section">
            <h3><span>02</span>{{ t('bills.daily') }}</h3>
            <p class="bill-scroll-hint bill-muted">{{ t('bills.scrollHint') }}</p>
            <div class="bill-table-scroll" tabindex="0" :aria-label="t('bills.daily')">
              <table class="bill-table bill-daily">
                <thead><tr><th>{{ t('bills.date') }}</th><th>{{ t('bills.credit') }}</th><th>{{ t('bills.transfers') }}</th><th>{{ t('bills.net') }}</th><th>{{ t('bills.actual') }}</th></tr></thead>
                <tbody>
                  <template v-for="day in bill.days" :key="day.date">
                    <tr><td><button class="bill-day-button" :aria-expanded="expanded.has(day.date)" :aria-controls="`day-${day.date}`" :aria-label="`${day.date} ${t('bills.details')}`" @click="toggleDay(day.date)">{{ day.date.slice(5) }} <span aria-hidden="true">{{ expanded.has(day.date) ? '−' : '+' }}</span></button></td><td>${{ money(day.consumption) }}</td><td>${{ money(day.rebate) }}</td><td>${{ money(day.net) }}</td><td>¥{{ money(day.net) }}</td></tr>
                    <tr :id="`day-${day.date}`" class="bill-day-detail" :class="{ 'bill-day-closed': !expanded.has(day.date), 'bill-day-zero': !day.models.length && !day.transfers.length }"><td colspan="5">
                      <p class="bill-detail-title">{{ day.date }} · {{ t('bills.details') }}</p>
                      <BillModelTable v-if="day.models.length" :models="day.models" :exact="exact" />
                      <p v-for="r in day.transfers" :key="r.id" class="bill-transfer">{{ t('bills.transfers') }} #{{ r.id }} · {{ dateTime(r.at) }} <strong>${{ money(r.amount) }}</strong></p>
                      <p v-if="!day.models.length && !day.transfers.length" class="bill-muted">{{ t('bills.noDailyUsage') }}</p>
                    </td></tr>
                  </template>
                </tbody>
                <tfoot><tr><th>{{ t('bills.total') }}</th><td>${{ money(bill.consumption) }}</td><td>${{ money(bill.rebate) }}</td><td>${{ money(bill.net) }}</td><td>¥{{ money(bill.cny) }}</td></tr></tfoot>
              </table>
            </div>
          </section>
          <footer class="bill-footer"><p>{{ app.siteName }} · {{ t('bills.statement') }}</p><p class="bill-muted">{{ t('bills.disclaimer') }}</p></footer>
        </article>
      </template>
    </div>
    <BaseDialog :show="confirmRegenerate" :title="t('bills.regenerateTitle')" @close="confirmRegenerate = false">
      <p>{{ t('bills.regenerateHint') }}</p>
      <template #footer><button class="btn btn-secondary" @click="confirmRegenerate = false">{{ t('common.cancel') }}</button><button class="btn btn-primary" data-test="bill-confirm-regenerate" @click="confirmRegenerate = false; generate()">{{ t('bills.regenerate') }}</button></template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import BillModelTable from '@/components/billing/BillModelTable.vue'
import { useAppStore } from '@/stores/app'
import { monthlyBillsAPI, type BillVersion, type MonthlyBill } from '@/api/monthlyBills'
import { billMoney, previousMonth, shanghaiMonth } from '@/utils/monthlyBill'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t, locale } = useI18n()
const app = useAppStore()
const currentMonth = shanghaiMonth()
const month = ref(previousMonth(currentMonth))
const monthOptions = computed(() => {
  const options: { value: string; label: string }[] = []
  let cursor = currentMonth
  while (cursor >= '2000-01') {
    options.push({ value: cursor, label: cursor })
    cursor = previousMonth(cursor)
  }
  return options
})
const validMonth = computed(() => /^\d{4}-(0[1-9]|1[0-2])$/.test(month.value) && month.value <= shanghaiMonth() && month.value >= '2000-01')
const versions = ref<BillVersion[]>([])
const version = ref(0)
const versionOptions = computed(() => versions.value.map(item => ({ value: item.version, label: `v${item.version} · ${dateTime(item.generated_at)}` })))
const bill = ref<MonthlyBill | null>(null)
const loading = ref(false)
const generating = ref(false)
const busy = computed(() => loading.value || generating.value)
const error = ref('')
const loadError = ref('')
const exact = ref(false)
const confirmRegenerate = ref(false)
const expanded = ref(new Set<string>())
let request = 0

function errorText(err: unknown): string {
  return extractApiErrorMessage(err, t('bills.error'))
}
function money(value: string) { return billMoney(value, exact.value) }
function dateTime(value: string) { return new Date(value).toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-GB', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }) }
function toggleDay(date: string) { if (expanded.value.has(date)) expanded.value.delete(date); else expanded.value.add(date) }

async function loadMonth() {
  const id = ++request
  bill.value = null; versions.value = []; version.value = 0; error.value = ''; loadError.value = ''; expanded.value.clear()
  if (!validMonth.value) { loading.value = false; return }
  loading.value = true
  try {
    const result = await monthlyBillsAPI.list(month.value)
    if (id !== request) return
    versions.value = result
    if (result[0]) {
      version.value = result[0].version
      const saved = await monthlyBillsAPI.get(month.value, version.value)
      if (id === request) bill.value = saved
    }
  } catch (err) { if (id === request) loadError.value = errorText(err) }
  finally { if (id === request) loading.value = false }
}
async function loadVersion() {
  const id = ++request
  loading.value = true; error.value = ''; loadError.value = ''; bill.value = null; expanded.value.clear()
  try { const saved = await monthlyBillsAPI.get(month.value, version.value); if (id === request) bill.value = saved }
  catch (err) { if (id === request) loadError.value = errorText(err) }
  finally { if (id === request) loading.value = false }
}
async function generate() {
  if (busy.value || !validMonth.value) return
  generating.value = true; error.value = ''
  try {
    const result = await monthlyBillsAPI.generate(month.value, versions.value[0]?.version || 0)
    bill.value = result; version.value = result.version; expanded.value.clear()
    if (!versions.value.some(v => v.version === result.version)) versions.value.unshift({ version: result.version, generated_at: result.generated_at })
  } catch (err) { error.value = errorText(err) }
  finally { generating.value = false }
}
function beforePrint() { document.body.classList.add('printing-bill') }
function afterPrint() { document.body.classList.remove('printing-bill') }
function printBill() { if (bill.value) { beforePrint(); window.print() } }
window.addEventListener('beforeprint', beforePrint)
window.addEventListener('afterprint', afterPrint)
onUnmounted(() => { request++; window.removeEventListener('beforeprint', beforePrint); window.removeEventListener('afterprint', afterPrint); afterPrint() })
watch(month, loadMonth, { immediate: true })
</script>

<style>
.bill-page { max-width: 1080px; margin: 0 auto; --bill-ink: #202c50; --bill-muted: #657493; --bill-rule: #d9e0eb; color: var(--bill-ink); }
.bill-scroll-hint { display: none; }
.bill-toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 20px; }
.bill-toolbar h1 { font-size: 18px; font-weight: 650; margin-right: auto; }
.bill-month { width: 168px; }
.bill-manual { font-size: 12px; color: var(--bill-muted); }
.bill-toolbar .btn { min-height: 42px; font-size: 12px; }
.bill-options { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin: 12px 0; color: var(--bill-muted); font-size: 12px; }
.bill-options label { display: flex; align-items: center; gap: 8px; }.bill-version { width: 280px; max-width: 100%; }
.bill-paper { padding: 48px 52px 32px; background: #fff; border: 1px solid #e6e9ee; border-radius: 6px; box-shadow: 0 8px 28px #182b4b06; }
.bill-heading { display: flex; justify-content: space-between; gap: 20px; align-items: flex-start; }.bill-heading > div:last-child { text-align: right; }
.bill-brand { display: flex; align-items: center; gap: 12px; font-size: 26px; font-weight: 750; overflow-wrap: anywhere; }.bill-brand img { width: 40px; height: 40px; object-fit: contain; }
.bill-eyebrow { font-size: 11px; letter-spacing: .13em; color: var(--bill-muted); margin: 0 0 6px; }.bill-heading h2 { font-size: 28px; font-weight: 700; letter-spacing: -.03em; }
.bill-partial { display: inline-block; padding: 4px 8px; margin-top: 8px; background: #eef3fd; color: #4e6598; font-size: 11px; border-radius: 3px; }
.bill-meta { display: grid; grid-template-columns: 1fr 1fr; gap: 32px; margin-top: 40px; font-size: 13px; line-height: 1.9; }.bill-meta strong { font-size: 17px; }.bill-email { overflow-wrap: anywhere; }.bill-meta dl { display: grid; grid-template-columns: auto 1fr; gap: 0 20px; justify-self: end; }.bill-meta dt { color: var(--bill-muted); }.bill-meta dd { margin: 0; }
.bill-muted { color: var(--bill-muted); font-size: 12px; line-height: 1.8; }
.bill-hero { display: grid; grid-template-columns: 1.15fr 1fr; align-items: center; gap: 30px; margin: 38px 0; }.bill-hero h3 { font-size: 16px; }.bill-amount { font-size: clamp(30px, 4vw, 52px); font-weight: 700; line-height: 1.3; letter-spacing: -.04em; margin: 9px 0; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }.bill-ratio { padding: 14px 0 14px 30px; border-left: 1px solid var(--bill-rule); font-size: 13px; }.bill-ratio strong { font-weight: 500; }.bill-ratio p { margin-top: 6px; }
.bill-section { margin-top: 32px; }.bill-section > h3 { font-weight: 650; font-size: 20px; margin-bottom: 16px; display: flex; gap: 16px; }.bill-section > h3 span { color: #6079b2; font-weight: 500; }
.bill-table-scroll { overflow-x: auto; }.bill-table { width: 100%; border-collapse: collapse; font-size: 12px; font-variant-numeric: tabular-nums; text-align: right; }.bill-table th { padding: 12px 10px; font-weight: 500; border-top: 1px solid #aabbd5; border-bottom: 1px solid #bdcbe0; }.bill-table td { padding: 13px 10px; border-bottom: 1px solid var(--bill-rule); }.bill-table th:first-child,.bill-table td:first-child { text-align: left; }.bill-models { table-layout: fixed; }.bill-models th:first-child { width: 32%; }.bill-models th:nth-child(2) { width: 44%; }.bill-models td { overflow-wrap: anywhere; }.bill-models td:nth-child(2) { color: var(--bill-muted); line-height: 1.8; }.bill-models td:last-child { font-weight: 500; }
.bill-settlement { display: grid; grid-template-columns: 1fr 1.1fr; gap: 40px; margin-top: 28px; align-items: end; }.bill-settlement > p { max-width: 290px; }.bill-settlement dl { display: grid; grid-template-columns: 1fr auto; font-size: 12px; border-top: 1px solid var(--bill-rule); font-variant-numeric: tabular-nums; }.bill-settlement dt,.bill-settlement dd { padding: 11px 10px; margin: 0; }.bill-settlement dd { text-align: right; overflow-wrap: anywhere; }.bill-rebate { color: #16845e; }.bill-net { border-top: 1px solid #aabbd5; }.bill-total { font-weight: 650; background: #f2f5fa; border-top: 1px solid #aabbd5; border-bottom: 3px double #aabbd5; }
.bill-daily { min-width: 570px; }.bill-daily td { padding-top: 3px; padding-bottom: 3px; }.bill-daily tfoot { font-weight: 650; background: #f2f5fa; }.bill-daily tfoot td { padding-top: 12px; padding-bottom: 12px; }.bill-day-button { min-height: 42px; display: flex; align-items: center; gap: 12px; border-radius: 4px; }.bill-day-button:hover { color: #335db4; }.bill-day-button span { color: var(--bill-muted); }.bill-day-closed { display: none; }.bill-day-detail > td { padding: 18px 12px !important; background: #f7f9fc; }.bill-detail-title { font-weight: 600; margin-bottom: 8px; }.bill-transfer { margin-top: 10px; font-size: 12px; }.bill-transfer strong { float: right; }.bill-subtotal { text-align: right; font-size: 13px; margin-top: 12px; }
.bill-footer { margin-top: 40px; padding-top: 18px; border-top: 1px solid var(--bill-rule); font-size: 12px; }.bill-footer p + p { margin-top: 4px; }.bill-empty { padding: 90px 24px; text-align: center; background: #fff; border: 1px solid var(--bill-rule); border-radius: 6px; }.bill-empty h2 { margin: 12px 0; font-size: 24px; font-weight: 600; }.bill-empty > p:last-child { max-width: 440px; margin: 0 auto; font-size: 14px; line-height: 1.9; color: var(--bill-muted); }.bill-error { padding: 14px; margin: 12px 0; border: 1px solid #f6a8a8; border-radius: 6px; background: #fff4f4; color: #a12b2b; font-size: 13px; }
.dark .bill-page { --bill-ink: #e0e8fa; --bill-muted: #acb8d1; --bill-rule: #38455b; }.dark .bill-paper,.dark .bill-empty { background: #182132; border-color: #38455b; }.dark .bill-total,.dark .bill-daily tfoot,.dark .bill-day-detail > td { background: #222e43; }.dark .bill-rebate { color: #63d5ac; }.dark .bill-partial { background: #293955; color: #b1c7fa; }
@media (max-width: 640px) {
  .bill-scroll-hint { display: block; margin: -8px 0 12px; }
  .bill-toolbar { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); }
  .bill-toolbar h1 { grid-column: 1 / -1; }.bill-month { grid-column: span 3; width: auto; }.bill-manual { grid-column: span 3; }
  .bill-toolbar .btn { grid-column: span 3; white-space: nowrap; padding-left: 6px; padding-right: 6px; font-size: 11px; }
  .bill-paper { padding: 24px 18px; }.bill-toolbar { gap: 8px; }.bill-toolbar h1 { flex-basis: 100%; }.bill-manual { margin-right: auto; }.bill-toolbar .btn { flex: 1; }.bill-brand { font-size: 18px; gap: 6px; }.bill-brand img { width: 28px; height: 28px; }.bill-heading h2 { font-size: 21px; }.bill-eyebrow { font-size: 9px; }.bill-meta { grid-template-columns: 1fr; gap: 18px; margin-top: 26px; }.bill-meta dl { justify-self: start; }.bill-hero { grid-template-columns: 1fr; gap: 20px; margin: 28px 0; }.bill-ratio { padding: 0 0 0 16px; }.bill-settlement { grid-template-columns: 1fr; gap: 20px; }.bill-settlement > p { max-width: none; }.bill-section > h3 { font-size: 18px; }.bill-models { min-width: 320px; }.bill-table td,.bill-table th { padding-left: 6px; padding-right: 6px; }.bill-version { width: 100%; }.bill-amount { font-size: 38px; }
}
@media print {
  @page { size: A4; margin: 14mm; @bottom-right { content: counter(page) " / " counter(pages); font-family: sans-serif; font-size: 9px; color: #657493; } }
  body.printing-bill { background: white !important; }
  body.printing-bill .app-shell > :not(.app-frame),body.printing-bill .app-header,body.printing-bill .bill-toolbar,body.printing-bill .bill-options,body.printing-bill .bill-error,body.printing-bill .bill-empty,body.printing-bill .modal-overlay { display: none !important; }
  body.printing-bill .app-frame,body.printing-bill .app-main { margin: 0 !important; padding: 0 !important; width: 100% !important; min-height: 0 !important; }
  body.printing-bill .bill-page { --bill-ink: #202c50; --bill-muted: #52617c; --bill-rule: #d9e0eb; color: var(--bill-ink); }
  body.printing-bill .bill-paper { padding: 0; border: 0; box-shadow: none; background: white; }
  body.printing-bill .bill-scroll-hint { display: none; }
  body.printing-bill .bill-meta { margin-top: 28px; }body.printing-bill .bill-hero { margin: 28px 0; }
  body.printing-bill .bill-amount { font-size: 48px; }
  body.printing-bill .bill-table th { padding-top: 8px; padding-bottom: 8px; }
  body.printing-bill .bill-table td { padding-top: 7px; padding-bottom: 7px; }
  body.printing-bill .bill-day-button { min-height: 0; padding: 2px 0; }
  body.printing-bill .bill-daily tfoot { display: table-row-group; }
  body.printing-bill .bill-footer { break-inside: avoid; margin-top: 24px; }
  body.printing-bill .bill-table-scroll { overflow: visible; }.bill-table thead { display: table-header-group; }
  body.printing-bill .bill-day-closed { display: table-row; }body.printing-bill .bill-day-zero { display: none; }
  body.printing-bill .bill-total,body.printing-bill .bill-daily tfoot,body.printing-bill .bill-day-detail > td { background: #f4f6fa; }
  body.printing-bill .bill-rebate { color: #16845e; }body.printing-bill .bill-day-button span { display: none; }
  body.printing-bill .bill-table { min-width: 0; }body.printing-bill .bill-table tr { break-inside: avoid; }body.printing-bill .bill-day-detail { break-inside: auto; }body.printing-bill .bill-section > h3 { break-after: avoid; }body.printing-bill .bill-heading,body.printing-bill .bill-hero,body.printing-bill .bill-settlement { break-inside: avoid; }
}
</style>
