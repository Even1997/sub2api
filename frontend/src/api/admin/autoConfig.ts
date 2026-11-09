import type { OpenAITwoFAAdmissionConfig } from '@/utils/openaiTwoFAAdmission'
import { list as listGroups } from './groups'
import type { AdminGroup } from '@/types'
import type { ScheduledTestPlan } from '@/types'
import { apiClient } from '../client'
import type { ModelBillingConfig } from '@/utils/modelBilling'
import type { ExcelBPSDefaults } from '@/utils/excelBPSDefaults'
import type { OAuthModelMappingRule } from '@/utils/oauthModelMappings'
import type { OpenAIWSMode } from '@/utils/openaiWsMode'
export type OAuthInitialQualityRule = Pick<ScheduledTestPlan, 'model_id' | 'cron_expression' | 'enabled' | 'max_results' | 'pelican_config'>
export interface OAuthInitialAccountSettings {
  rate_multiplier?: number | null
  cost_multiplier?: number | null
  proxy_id?: number | null
  auto_pause_on_expired?: boolean | null
  openai_passthrough?: boolean | null
  openai_flatten_namespaces?: boolean | null
  openai_long_context_billing?: boolean | null
  codex_cli_only?: boolean | null
  codex_cli_only_app_server?: boolean | null
  codex_fingerprint_mode?: string | null
  openai_compact_mode?: string | null
  openai_request_timezone?: string | null
  slot_reservation_enabled?: boolean | null
  slot_reservation_seconds?: number | null
  openai_ws_mode?: string | null
  openai_ws_sse_acceleration?: boolean | null
  codex_image_tool_mode?: 'inherit' | 'enabled' | 'disabled' | 'block' | null
  plan_type?: string | null
  openai_model_aliases?: boolean | null
  openai_compact_model_mapping?: OAuthCompactModelMapping[] | null
  auto_pause_5h_disabled?: boolean | null
  auto_pause_7d_disabled?: boolean | null
  auto_pause_5h_threshold?: number | null
  auto_pause_7d_threshold?: number | null
  auto_reset_credit_enabled?: boolean | null
  auto_reset_credit_5h_threshold?: number | null
  auto_reset_credit_7d_threshold?: number | null
  openai_prism_browser?: boolean | null
  openai_prism_browser_models?: string[] | null
  intercept_warmup?: boolean | null
}
export interface OAuthCompactModelMapping {
  from: string
  to: string
}
export interface AutoConfig {
 degradation_action?: import('@/utils/oauthDegradation').OAuthDegradationConfig
 openai_two_fa_admission?: OpenAITwoFAAdmissionConfig
 openai_totp_rotate_on_429?: boolean
 openai_disable_scheduling_on_429?: boolean
 openai_ws_mode?: OpenAIWSMode
 openai_ws_sse_acceleration?: boolean
 quality_rule?: OAuthInitialQualityRule | null
 selected_models?: string[] | null
 model_mappings?: OAuthModelMappingRule[] | null
 model_billing?: ModelBillingConfig
 excel_bps?: ExcelBPSDefaults
 account_settings?: OAuthInitialAccountSettings
 enabled: boolean
 platform: string
 priority: number
 load_factor: number
 concurrency: number
 group_ids: number[]
 upgrade_enabled: boolean
 upgrade_group_ids: number[]
 successes_per_step: number
 upgrade_step: number
 max_concurrency: number
 cooldown_seconds: number
 revision: string
 runtime_blocked?: boolean
}
export async function getAutoConfig(): Promise<AutoConfig> { return (await apiClient.get('/admin/account-ops/auto-config')).data }
export async function saveAutoConfig(config: AutoConfig): Promise<AutoConfig> { return (await apiClient.put('/admin/account-ops/auto-config', config)).data }

export type AutoConfigEventKind = 'config_saved' | 'initial_applied' | 'concurrency_upgraded' | 'failure_cooldown' | 'degradation_applied'
export interface AutoConfigEvent {
  id: number
  account_id: number
  account_name: string
  platform: string
  kind: AutoConfigEventKind
  created_at: string
  details: {
    degradation?: { revision: string; limit_concurrency?: boolean; limit_models?: boolean; limit_groups?: boolean; models: string[] | null; group_ids?: number[]; removed_group_ids?: number[]; threshold_percent: number; degradation_percent: number; quality_samples: number; window_start: string; window_end: string; concurrency_cap: number }
    model_mapping?: Record<string, string>
    config?: AutoConfig
    priority: number
    load_factor: number
    concurrency: number
    previous_concurrency: number
    group_ids?: number[]
    cooldown_seconds: number
  }
}
export interface AutoConfigEventsPage {
  items: AutoConfigEvent[]
  has_more: boolean
}
export async function getAutoConfigEvents(params: { before?: number; kind?: AutoConfigEventKind | ''; limit?: number } = {}): Promise<AutoConfigEventsPage> {
  return (await apiClient.get('/admin/account-ops/auto-config/events', { params })).data
}

// Collect every page before exposing scope options. A partial list must never
// turn selected IDs into an empty (global) scope.
export async function getModelBillingGroups(): Promise<AdminGroup[]> {
 const groups = new Map<number, AdminGroup>()
 for (let page = 1; ; page++) {
  const batch = await listGroups(page, 100, { status: 'active', sort_by: 'id', sort_order: 'asc' })
  if (batch.page !== page || !Number.isSafeInteger(batch.pages) || batch.pages < 0 ||
      !Number.isSafeInteger(batch.total) || batch.total < 0) throw new Error('Incomplete group list')
  for (const group of batch.items) groups.set(group.id, group)
  if (page >= batch.pages) {
   if (groups.size < batch.total) throw new Error('Incomplete group list')
   return [...groups.values()]
  }
  if (!batch.items.length) throw new Error('Incomplete group list')
 }
}
