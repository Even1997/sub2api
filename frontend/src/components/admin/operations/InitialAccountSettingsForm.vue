<template>
 <div class="space-y-4">
  <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
   <div>
    <label class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}</label>
    <input data-testid="account-rate" class="input w-full" type="number" min="0" step="0.001" :placeholder="t('autoConfig.account.unset')" :value="numberText('rate_multiplier')" @input="setNumber('rate_multiplier', $event)" />
    <p class="input-hint">{{ t('admin.accounts.billingRateMultiplierHint') }}</p>
   </div>
   <div>
    <label class="input-label" for="initial-cost-multiplier">{{ t('admin.accounts.costMultiplier') }}</label>
    <input id="initial-cost-multiplier" data-testid="account-cost" class="input w-full" type="number" min="0" max="1000000" step="0.001" :placeholder="t('autoConfig.account.unset')" :value="numberText('cost_multiplier')" @input="setNumber('cost_multiplier', $event)" />
    <p class="input-hint">{{ t('admin.accounts.costMultiplierHint') }}</p>
   </div>
  </div>

  <div>
   <label class="input-label">{{ t('admin.accounts.proxy') }}</label>
   <select data-testid="account-proxy" class="input w-full" :value="proxyText" @change="write('proxy_id', eventNumber($event))">
    <option value="">{{ t('autoConfig.account.unset') }}</option>
    <option value="0">{{ t('autoConfig.account.noProxy') }}</option>
    <option v-for="proxy in proxies" :key="proxy.id" :value="String(proxy.id)">{{ proxy.name }} · #{{ proxy.id }}</option>
   </select>
  </div>

  <div class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600">
   <div class="min-w-0">
    <label class="input-label mb-0">{{ t('admin.accounts.autoPauseOnExpired') }}</label>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.autoPauseOnExpiredDesc') }}</p>
   </div>
   <TriSwitch test-id="account-auto-pause" :value="boolValue('auto_pause_on_expired')" @change="write('auto_pause_on_expired', $event)" />
  </div>

  <div v-if="platform === 'openai'" class="space-y-4" data-testid="account-openai-settings">
   <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
    <div class="flex items-center justify-between gap-3">
     <label class="text-sm">{{ t('priorityScheduling.modelAliases') }}</label>
     <TriSwitch test-id="account-model-aliases" :value="boolValue('openai_model_aliases')" @change="write('openai_model_aliases', $event)" />
    </div>
    <p class="input-hint">{{ t('priorityScheduling.modelAliasesHint') }}</p>
   </div>

   <SettingRow :title="t('admin.accounts.openai.oauthPassthrough')" :description="t('admin.accounts.openai.oauthPassthroughDesc')">
    <TriSwitch test-id="account-openai_passthrough" :value="boolValue('openai_passthrough')" @change="write('openai_passthrough', $event)" />
   </SettingRow>
   <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <label class="input-label">{{ t('admin.accounts.openai.requestTimezone') }}</label>
    <input data-testid="account-timezone" class="input w-full" type="text" maxlength="64" placeholder="Asia/Singapore" :value="textValue('openai_request_timezone')" @input="write('openai_request_timezone', eventText($event))" />
    <p class="input-hint">{{ t('admin.accounts.openai.requestTimezoneDesc') }}</p>
   </div>
   <SettingRow :title="t('admin.accounts.openai.flattenNamespaces')" :description="t('admin.accounts.openai.flattenNamespacesDesc')">
    <TriSwitch test-id="account-openai_flatten_namespaces" :value="boolValue('openai_flatten_namespaces')" @change="write('openai_flatten_namespaces', $event)" />
   </SettingRow>

   <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="overflow-hidden rounded-lg border border-sky-100 bg-sky-50/60 shadow-sm dark:border-sky-900/50 dark:bg-sky-950/20">
     <div class="flex items-start justify-between gap-3 px-4 py-3">
      <div class="min-w-0">
       <div class="flex flex-wrap items-center gap-2">
        <label class="input-label mb-0">{{ t('admin.accounts.openai.codexImageTool') }}</label>
        <span class="rounded-full px-2 py-0.5 text-[11px] font-medium" :class="imageBadgeClass">{{ imageBadge }}</span>
       </div>
       <p class="mt-1 text-xs leading-5 text-slate-600 dark:text-slate-300">{{ t('admin.accounts.openai.codexImageToolDesc') }}</p>
      </div>
      <button v-if="textValue('codex_image_tool_mode')" type="button" class="shrink-0 text-xs text-gray-500 underline" @click="write('codex_image_tool_mode', null)">{{ t('autoConfig.account.unset') }}</button>
     </div>
     <div class="border-t border-sky-100 bg-white/70 p-2 dark:border-sky-900/50 dark:bg-dark-800/70">
      <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
       <button v-for="option in imageOptions" :key="option.value" type="button" :data-testid="`codex-image-tool-${option.value}`" :class="['flex min-h-[62px] items-start gap-2 rounded-md border px-3 py-2 text-left', textValue('codex_image_tool_mode') === option.value ? option.selected : 'border-transparent text-slate-600 hover:border-gray-200 hover:bg-gray-50 dark:text-slate-300']" @click="write('codex_image_tool_mode', option.value)">
        <span class="min-w-0"><span class="block text-sm font-medium">{{ option.label }}</span><span class="mt-0.5 block text-xs leading-4 text-slate-500 dark:text-slate-400">{{ option.description }}</span></span>
       </button>
      </div>
     </div>
    </div>
   </div>

   <SettingRow :title="t('admin.accounts.openai.longContextBilling')" :description="t('admin.accounts.openai.longContextBillingDesc')">
    <TriSwitch test-id="account-openai_long_context_billing" :value="boolValue('openai_long_context_billing')" @change="write('openai_long_context_billing', $event)" />
   </SettingRow>
   <SettingRow :title="t('admin.accounts.openai.codexCLIOnly')" :description="t('admin.accounts.openai.codexCLIOnlyDesc')">
    <TriSwitch test-id="account-codex_cli_only" :value="boolValue('codex_cli_only')" @change="setCLI" />
   </SettingRow>
   <SettingRow v-if="boolValue('codex_cli_only') === true" :title="t('admin.accounts.openai.codexCLIOnlyAppServer')" :description="t('admin.accounts.openai.codexCLIOnlyAppServerDesc')">
    <TriSwitch test-id="account-app-server" :value="boolValue('codex_cli_only_app_server')" @change="write('codex_cli_only_app_server', $event)" />
   </SettingRow>
   <SettingRow :title="t('admin.accounts.openai.codexFingerprintMode')" :description="t('admin.accounts.openai.codexFingerprintModeDesc')">
    <select data-testid="account-fingerprint" class="input w-44 shrink-0" :value="textValue('codex_fingerprint_mode')" @change="write('codex_fingerprint_mode', eventText($event))">
     <option value="">{{ t('autoConfig.account.unset') }}</option>
     <option v-for="mode in fingerprintModes" :key="mode.value" :value="mode.value">{{ mode.label }}</option>
    </select>
   </SettingRow>
   <SettingRow :title="t('admin.accounts.openai.planType')" :description="t('admin.accounts.openai.planTypeDesc')">
    <select data-testid="account-plan-type" class="input w-52 shrink-0" :value="textValue('plan_type')" @change="write('plan_type', eventText($event))">
     <option value="">{{ t('autoConfig.account.unset') }}</option>
     <option v-for="plan in planTypes" :key="plan" :value="plan">{{ planLabel(plan) }}</option>
    </select>
   </SettingRow>
   <SettingRow :title="t('admin.accounts.openai.compactMode')" :description="t('admin.accounts.openai.compactModeDesc')">
    <select data-testid="account-compact" class="input w-44 shrink-0" :value="textValue('openai_compact_mode')" @change="write('openai_compact_mode', eventText($event))">
     <option value="">{{ t('autoConfig.account.unset') }}</option>
     <option value="auto">{{ t('admin.accounts.openai.compactModeAuto') }}</option>
     <option value="force_on">{{ t('admin.accounts.openai.compactModeForceOn') }}</option>
     <option value="force_off">{{ t('admin.accounts.openai.compactModeForceOff') }}</option>
    </select>
   </SettingRow>

   <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
    <div>
     <label class="input-label mb-0">{{ t('admin.accounts.openai.compactModelMapping') }}</label>
     <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.openai.compactModelMappingDesc') }}</p>
    </div>
    <div v-for="(rule, index) in compactMappings" :key="index" class="grid grid-cols-[1fr_1fr_auto] gap-2">
     <input class="input" :aria-label="t('admin.accounts.fromModel')" :placeholder="t('admin.accounts.fromModel')" :value="rule.from" @input="setCompact(index, 'from', $event)" />
     <input class="input" :aria-label="t('admin.accounts.toModel')" :placeholder="t('admin.accounts.toModel')" :value="rule.to" @input="setCompact(index, 'to', $event)" />
     <button type="button" class="btn btn-secondary btn-sm" @click="removeCompact(index)">{{ t('common.delete') }}</button>
    </div>
    <button type="button" class="btn btn-secondary btn-sm" data-testid="account-add-compact-mapping" @click="addCompact">{{ t('admin.accounts.addMapping') }}</button>
   </div>

   <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
    <SettingRow bare :title="t('admin.accounts.autoPause5hDisabled')" :description="t('admin.accounts.autoPauseDisabledHint')">
     <TriSwitch test-id="account-auto-pause-5h" :value="boolValue('auto_pause_5h_disabled')" @change="write('auto_pause_5h_disabled', $event)" />
    </SettingRow>
    <div>
     <label class="input-label">{{ t('admin.accounts.autoPause5hThreshold') }}</label>
     <input data-testid="account-auto-pause-5h-threshold" class="input" type="number" min="0" max="100" step="0.1" :placeholder="t('autoConfig.account.unset')" :disabled="boolValue('auto_pause_5h_disabled') === true" :value="percentText('auto_pause_5h_threshold')" @input="setPercent('auto_pause_5h_threshold', $event)" />
     <p class="input-hint">{{ t('admin.accounts.autoPauseThresholdHint') }}</p>
    </div>
    <SettingRow bare :title="t('admin.accounts.autoPause7dDisabled')" :description="t('admin.accounts.autoPauseDisabledHint')">
     <TriSwitch test-id="account-auto-pause-7d" :value="boolValue('auto_pause_7d_disabled')" @change="write('auto_pause_7d_disabled', $event)" />
    </SettingRow>
    <div>
     <label class="input-label">{{ t('admin.accounts.autoPause7dThreshold') }}</label>
     <input data-testid="account-auto-pause-7d-threshold" class="input" type="number" min="0" max="100" step="0.1" :placeholder="t('autoConfig.account.unset')" :disabled="boolValue('auto_pause_7d_disabled') === true" :value="percentText('auto_pause_7d_threshold')" @input="setPercent('auto_pause_7d_threshold', $event)" />
    </div>
   </div>

   <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600">
    <SettingRow bare :title="t('admin.accounts.autoResetCredit.title')" :description="t('admin.accounts.autoResetCredit.hint')">
     <TriSwitch test-id="account-auto-reset" :value="boolValue('auto_reset_credit_enabled')" @change="setReset" />
    </SettingRow>
    <div v-if="boolValue('auto_reset_credit_enabled') === true" class="grid gap-3 sm:grid-cols-2">
     <div>
      <label class="input-label">{{ t('admin.accounts.autoResetCredit.threshold5h') }}</label>
      <input data-testid="account-auto-reset-5h" class="input" type="number" min="0" max="100" step="0.1" :value="percentText('auto_reset_credit_5h_threshold')" @input="setPercent('auto_reset_credit_5h_threshold', $event)" />
     </div>
     <div>
      <label class="input-label">{{ t('admin.accounts.autoResetCredit.threshold7d') }}</label>
      <input data-testid="account-auto-reset-7d" class="input" type="number" min="0" max="100" step="0.1" :value="percentText('auto_reset_credit_7d_threshold')" @input="setPercent('auto_reset_credit_7d_threshold', $event)" />
     </div>
    </div>
    <p class="input-hint">{{ t('admin.accounts.autoResetCredit.thresholdHint') }}</p>
   </div>

   <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="flex items-center justify-between gap-3">
     <span class="text-sm">{{ t('admin.accounts.openai.prismBrowser') }}</span>
     <TriSwitch test-id="account-prism" :value="boolValue('openai_prism_browser')" @change="setPrism" />
    </div>
    <p class="input-hint">{{ t('admin.accounts.openai.prismBrowserDesc') }}</p>
    <fieldset v-if="boolValue('openai_prism_browser') === true" class="mt-3">
     <legend class="input-label">{{ t('admin.accounts.openai.prismBrowserModels') }}</legend>
     <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
      <label v-for="model in prismModels" :key="model" class="flex items-center gap-2 text-sm">
       <input type="checkbox" :checked="prismSelected(model)" :data-testid="`prism-model-${model}`" @change="togglePrismModel(model, $event)" />
       <span>{{ model }}</span>
      </label>
     </div>
     <p class="input-hint">{{ t('admin.accounts.openai.prismBrowserModelsHint') }}</p>
    </fieldset>
   </div>

   <div class="rounded-lg border border-dashed border-gray-300 p-3 dark:border-dark-500">
    <div class="flex items-center justify-between gap-3">
     <label class="input-label mb-0">{{ t('admin.accounts.slotReservation.title') }}</label>
     <TriSwitch test-id="account-slot_reservation_enabled" :value="boolValue('slot_reservation_enabled')" @change="setSlot" />
    </div>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.slotReservation.hint') }}</p>
    <label v-if="boolValue('slot_reservation_enabled') === true" class="mt-3 block">
     <span class="input-label">{{ t('admin.accounts.slotReservation.seconds') }}</span>
     <input data-testid="account-slot-seconds" class="input w-full" type="number" min="1" max="3600" step="1" :value="numberText('slot_reservation_seconds')" @input="setNumber('slot_reservation_seconds', $event)" />
    </label>
   </div>
  </div>

  <SettingRow v-if="platform === 'anthropic' || platform === 'antigravity'" :title="t('admin.accounts.interceptWarmupRequests')" :description="t('admin.accounts.interceptWarmupRequestsDesc')">
   <TriSwitch test-id="account-warmup" :value="boolValue('intercept_warmup')" @change="write('intercept_warmup', $event)" />
  </SettingRow>
 </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OAuthInitialAccountSettings } from '@/api/admin/autoConfig'
import type { Proxy } from '@/types'
import { openAIPlanTypeLabel, openAIPlanTypes } from '@/utils/planType'
import SettingRow from './InitialAccountSettingRow.vue'
import TriSwitch from './InitialAccountTriSwitch.vue'

const model = defineModel<OAuthInitialAccountSettings | undefined>()
defineProps<{ platform: string; proxies: Proxy[] }>()
const { t } = useI18n()
const prismModels = ['gpt-6.1-sol', 'gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-6-luna']
const planTypes = openAIPlanTypes
const planLabel = (value: string) => openAIPlanTypeLabel(value) || value
const fingerprintModes = [
 { value: 'off', label: t('admin.accounts.openai.codexFingerprintOff') },
 { value: 'device', label: t('admin.accounts.openai.codexFingerprintDevice') },
 { value: 'session', label: t('admin.accounts.openai.codexFingerprintSession') },
 { value: 'full', label: t('admin.accounts.openai.codexFingerprintFull') },
]
const imageOptions = computed(() => [
 { value: 'inherit', label: t('admin.accounts.openai.codexImageToolInherit'), description: t('admin.accounts.openai.codexImageToolInheritDesc'), selected: 'border-sky-300 bg-sky-50 text-sky-900' },
 { value: 'enabled', label: t('admin.accounts.openai.codexImageToolEnabled'), description: t('admin.accounts.openai.codexImageToolEnabledDesc'), selected: 'border-emerald-300 bg-emerald-50 text-emerald-900' },
 { value: 'disabled', label: t('admin.accounts.openai.codexImageToolDisabled'), description: t('admin.accounts.openai.codexImageToolDisabledDesc'), selected: 'border-amber-300 bg-amber-50 text-amber-900' },
 { value: 'block', label: t('admin.accounts.openai.codexImageToolBlock'), description: t('admin.accounts.openai.codexImageToolBlockDesc'), selected: 'border-rose-300 bg-rose-50 text-rose-900' },
])
const imageBadge = computed(() => {
 switch (textValue('codex_image_tool_mode')) {
  case 'enabled': return t('admin.accounts.openai.codexImageToolBadgeEnabled')
  case 'disabled': return t('admin.accounts.openai.codexImageToolBadgeDisabled')
  case 'block': return t('admin.accounts.openai.codexImageToolBadgeBlock')
  case 'inherit': return t('admin.accounts.openai.codexImageToolBadgeInherit')
  default: return t('autoConfig.account.unset')
 }
})
const imageBadgeClass = computed(() => {
 switch (textValue('codex_image_tool_mode')) {
  case 'enabled': return 'bg-emerald-100 text-emerald-700'
  case 'disabled': return 'bg-amber-100 text-amber-700'
  case 'block': return 'bg-rose-100 text-rose-700'
  case 'inherit': return 'bg-sky-100 text-sky-700'
  default: return 'bg-gray-100 text-gray-600'
 }
})
const compactMappings = computed(() => model.value?.openai_compact_model_mapping ?? [])
const proxyText = computed(() => model.value?.proxy_id == null ? '' : String(model.value.proxy_id))

function assign(next: OAuthInitialAccountSettings, key: keyof OAuthInitialAccountSettings, value: OAuthInitialAccountSettings[keyof OAuthInitialAccountSettings] | null) {
 if (value == null || value === '' || (Array.isArray(value) && value.length === 0)) delete next[key]
 else (next as Record<string, unknown>)[key] = value
}
function commit(next: OAuthInitialAccountSettings) { model.value = Object.keys(next).length ? next : undefined }
function write(key: keyof OAuthInitialAccountSettings, value: OAuthInitialAccountSettings[keyof OAuthInitialAccountSettings] | null) {
 const next: OAuthInitialAccountSettings = { ...(model.value ?? {}) }
 assign(next, key, value)
 commit(next)
}
function boolValue(key: keyof OAuthInitialAccountSettings) { return model.value?.[key] as boolean | null | undefined }
function textValue(key: keyof OAuthInitialAccountSettings) { const value = model.value?.[key]; return typeof value === 'string' ? value : '' }
function numberText(key: 'rate_multiplier' | 'cost_multiplier' | 'slot_reservation_seconds') { const value = model.value?.[key]; return value == null ? '' : value }
function percentText(key: 'auto_pause_5h_threshold' | 'auto_pause_7d_threshold' | 'auto_reset_credit_5h_threshold' | 'auto_reset_credit_7d_threshold') {
 const value = model.value?.[key]
 return value == null ? '' : String(Math.round(value * 1000) / 10)
}
function eventText(event: Event) { return event.target instanceof HTMLInputElement || event.target instanceof HTMLSelectElement ? event.target.value.trim() : '' }
function eventNumber(event: Event) { const raw = eventText(event); return raw === '' ? null : Number(raw) }
function setNumber(key: 'rate_multiplier' | 'cost_multiplier' | 'slot_reservation_seconds', event: Event) {
 const raw = eventText(event)
 write(key, raw === '' ? null : Number(raw))
}
function setPercent(key: 'auto_pause_5h_threshold' | 'auto_pause_7d_threshold' | 'auto_reset_credit_5h_threshold' | 'auto_reset_credit_7d_threshold', event: Event) {
 const raw = eventText(event)
 write(key, raw === '' ? null : Number(raw) / 100)
}
function setCLI(value: boolean | null) {
 const next: OAuthInitialAccountSettings = { ...(model.value ?? {}) }
 assign(next, 'codex_cli_only', value)
 if (value !== true) assign(next, 'codex_cli_only_app_server', null)
 commit(next)
}
function setSlot(value: boolean | null) {
 const next: OAuthInitialAccountSettings = { ...(model.value ?? {}) }
 assign(next, 'slot_reservation_enabled', value)
 assign(next, 'slot_reservation_seconds', value === true ? (model.value?.slot_reservation_seconds ?? 60) : null)
 commit(next)
}
function setReset(value: boolean | null) {
 const next: OAuthInitialAccountSettings = { ...(model.value ?? {}) }
 assign(next, 'auto_reset_credit_enabled', value)
 if (value === true) {
  assign(next, 'auto_reset_credit_5h_threshold', model.value?.auto_reset_credit_5h_threshold ?? 1)
  assign(next, 'auto_reset_credit_7d_threshold', model.value?.auto_reset_credit_7d_threshold ?? 1)
 }
 commit(next)
}
function setPrism(value: boolean | null) {
 const next: OAuthInitialAccountSettings = { ...(model.value ?? {}) }
 assign(next, 'openai_prism_browser', value)
 assign(next, 'openai_prism_browser_models', value === true ? (model.value?.openai_prism_browser_models?.length ? model.value.openai_prism_browser_models : [...prismModels]) : null)
 commit(next)
}
function prismSelected(name: string) { return model.value?.openai_prism_browser_models?.includes(name) ?? false }
function togglePrismModel(name: string, event: Event) {
 const checked = event.target instanceof HTMLInputElement && event.target.checked
 const current = model.value?.openai_prism_browser_models ?? []
 write('openai_prism_browser_models', checked ? [...current, name] : current.filter(item => item !== name))
}
function setCompact(index: number, field: 'from' | 'to', event: Event) {
 const next = compactMappings.value.map(rule => ({ ...rule }))
 next[index] = { ...next[index], [field]: eventText(event) }
 write('openai_compact_model_mapping', next)
}
function addCompact() { write('openai_compact_model_mapping', [...compactMappings.value, { from: '', to: '' }]) }
function removeCompact(index: number) { write('openai_compact_model_mapping', compactMappings.value.filter((_, item) => item !== index)) }
</script>
