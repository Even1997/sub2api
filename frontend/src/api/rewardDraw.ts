import { apiClient } from './client'

export type RewardDrawPoolID = 'breed' | 'profession' | 'treasure'

export interface RewardDrawPoolItem {
  tier_id: string
  name: string
  rarity: string
  probability: number
  wool: number
  is_jackpot: boolean
}

export interface RewardDrawPool {
  id: RewardDrawPoolID
  name: string
  items: RewardDrawPoolItem[]
}

export interface RewardDrawRecipe {
  id: string
  name: string
  wool: number
}

export interface RewardDrawComponent {
  id: number
  batch_id: number
  position: number
  pool_id: RewardDrawPoolID
  tier_id: string
  name: string
  rarity: string
  wool_value: number
  is_jackpot: boolean
  stack_count: number
  combined_at?: string
  redeemed_at?: string
  created_at: string
}

export interface RewardDrawCard {
  id: number
  batch_id?: number
  position: number
  breed_id: string
  breed_name: string
  profession_id: string
  profession_name: string
  treasure_id: string
  treasure_name: string
  breed_component_id?: number
  profession_component_id?: number
  treasure_component_id?: number
  recipe_id: string
  title: string
  wool_value: number
  is_jackpot: boolean
  redeemed_at?: string
  created_at: string
}

export interface RewardDrawCombineResult {
  cards: RewardDrawCard[]
  quantity: number
  recipe_id: string
  wool_total: number
}

export interface RewardDrawBatch {
  id: number
  request_key: string
  draw_count: number
  pool_version: number
  pool_id: RewardDrawPoolID | 'legacy'
  total_wool: number
  created_at: string
  components: RewardDrawComponent[]
  cards?: RewardDrawCard[]
}

export interface RewardDrawCollectionEntry {
  breed_id: string
  breed_name: string
  profession_id: string
  profession_name: string
  treasure_id: string
  treasure_name: string
  recipe_id: string
  title: string
  wool_value: number
  discovered_count: number
  available_count: number
  first_seen_at: string
}

export interface RewardDrawOverview {
  pool_version: number
  wallet: {
    user_id: number
    ticket_balance: number
    consumption_remainder_units: number
    lifetime_consumption_units: number
    lifetime_tickets: number
    lifetime_draws: number
    wool_redeemed: number
    reward_units_redeemed: number
    available_wool: number
    available_cards: number
    available_components: number
    discovered_combinations: number
    user_balance: number
    consumption_progress: number
    available_reward: number
    redeemed_reward: number
  }
  pools: RewardDrawPool[]
  recipes: RewardDrawRecipe[]
  collection: RewardDrawCollectionEntry[]
  total_combinations: number
  wool_per_credit: number
  expected_reward_per_draw: number
  theoretical_payout_rate: number
  consumption_per_draw: number
  maximum_draw_count: number
  minimum_redemption_reward: number
}

export interface RewardDrawComponentPage {
  items: RewardDrawComponent[]
  total: number
  limit: number
  offset: number
}

export interface RewardDrawCardPage {
  items: RewardDrawCard[]
  total: number
  limit: number
  offset: number
}

export interface RewardDrawHistoryPage {
  items: RewardDrawBatch[]
  total: number
  limit: number
  offset: number
}

export interface RewardDrawResult {
  batch: RewardDrawBatch
  recovered: boolean
}

export interface RewardDrawRedemption {
  id: number
  redeem_code_id: number
  redeem_code: string
  component_count: number
  card_count: number
  wool: number
  reward: number
  status: string
  used_at?: string
  created_at: string
}

export interface RewardDrawRedemptionPage {
  items: RewardDrawRedemption[]
  total: number
  limit: number
  offset: number
}

export async function getRewardDrawOverview(): Promise<RewardDrawOverview> {
  const { data } = await apiClient.get<RewardDrawOverview>('/reward-draw')
  return data
}

export async function drawRewardCards(count: number, poolId: RewardDrawPoolID, requestId: string): Promise<RewardDrawResult> {
  const { data } = await apiClient.post<RewardDrawResult>('/reward-draw/draw', {
    count,
    pool_id: poolId,
    request_id: requestId,
  })
  return data
}

export async function getRewardDrawComponents(limit = 300): Promise<RewardDrawComponentPage> {
  const { data } = await apiClient.get<RewardDrawComponentPage>('/reward-draw/components', {
    params: { available_only: true, limit },
  })
  return data
}

export async function combineRewardDrawComponents(componentIds: number[], quantity = 1): Promise<RewardDrawCombineResult> {
  const { data } = await apiClient.post<RewardDrawCombineResult>('/reward-draw/combine', {
    component_ids: componentIds,
    quantity,
  })
  return data
}

export async function getRewardDrawCards(limit = 300): Promise<RewardDrawCardPage> {
  const { data } = await apiClient.get<RewardDrawCardPage>('/reward-draw/cards', {
    params: { available_only: true, limit },
  })
  return data
}

export async function getRewardDrawHistory(limit = 30): Promise<RewardDrawHistoryPage> {
  const { data } = await apiClient.get<RewardDrawHistoryPage>('/reward-draw/history', {
    params: { limit },
  })
  return data
}

export async function getRewardDrawRedemptions(limit = 30): Promise<RewardDrawRedemptionPage> {
  const { data } = await apiClient.get<RewardDrawRedemptionPage>('/reward-draw/redemptions', {
    params: { limit },
  })
  return data
}

export async function redeemRewardDrawItems(componentIds: number[] = [], cardIds: number[] = [], redeemAll = false): Promise<RewardDrawRedemption> {
  const { data } = await apiClient.post<RewardDrawRedemption>('/reward-draw/redeem', {
    component_ids: componentIds,
    card_ids: cardIds,
    all: redeemAll,
  })
  return data
}
