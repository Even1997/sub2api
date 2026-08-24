import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'

import RewardDrawView from '../RewardDrawView.vue'

const {
  combineRewardDrawComponents,
  drawRewardCards,
  getRewardDrawCards,
  getRewardDrawComponents,
  getRewardDrawHistory,
  getRewardDrawOverview,
  getRewardDrawRedemptions,
  redeemRewardDrawItems,
} = vi.hoisted(() => ({
  combineRewardDrawComponents: vi.fn(),
  drawRewardCards: vi.fn(),
  getRewardDrawCards: vi.fn(),
  getRewardDrawComponents: vi.fn(),
  getRewardDrawHistory: vi.fn(),
  getRewardDrawOverview: vi.fn(),
  getRewardDrawRedemptions: vi.fn(),
  redeemRewardDrawItems: vi.fn(),
}))

vi.mock('@/api/rewardDraw', () => ({
  combineRewardDrawComponents,
  drawRewardCards,
  getRewardDrawCards,
  getRewardDrawComponents,
  getRewardDrawHistory,
  getRewardDrawOverview,
  getRewardDrawRedemptions,
  redeemRewardDrawItems,
}))

const poolItem = (name: string) => ({ tier_id: 'code', name, rarity: 'N', probability: 0.399999, wool: 7, is_jackpot: false })
const overview = {
  pool_version: 3,
  wallet: {
    user_id: 7,
    ticket_balance: 25,
    consumption_remainder_units: 250000,
    lifetime_consumption_units: 25000000,
    lifetime_tickets: 25,
    lifetime_draws: 0,
    wool_redeemed: 0,
    reward_units_redeemed: 0,
    available_wool: 0,
    available_cards: 0,
    available_components: 0,
    discovered_combinations: 0,
    user_balance: 128.6,
    consumption_progress: 0.25,
    available_reward: 0,
    redeemed_reward: 0,
  },
  pools: [
    { id: 'breed', name: '羊种池', items: [poolItem('绒云羊')] },
    { id: 'profession', name: '职业池', items: [poolItem('牧场学徒')] },
    { id: 'treasure', name: '宝物池', items: [poolItem('棉花羊铃')] },
  ],
  recipes: [
    { id: 'code', name: '绒云咩', wool: 32 },
    { id: 'reasoning', name: '侦探咩', wool: 41 },
    { id: 'speed', name: '极速咩', wool: 50 },
    { id: 'inspiration', name: '灵感咩', wool: 59 },
    { id: 'wisdom', name: '智慧咩', wool: 90 },
    { id: 'fortune', name: '招财咩', wool: 180 },
    { id: 'miracle', name: '奇迹咩', wool: 100000 },
  ],
  collection: [],
  total_combinations: 7,
  wool_per_credit: 1000,
  expected_reward_per_draw: 0.010009993,
  theoretical_payout_rate: 0.015123323,
  consumption_per_draw: 1,
  maximum_draw_count: 100,
  minimum_redemption_reward: 0.001,
}

const component = (
  id: number,
  pool_id: 'breed' | 'profession' | 'treasure',
  name: string,
  tier_id = 'code',
  wool_value = 7,
  stack_count = 1,
) => ({
  id,
  batch_id: id,
  position: 0,
  pool_id,
  tier_id,
  name,
  rarity: 'N',
  wool_value,
  stack_count,
  is_jackpot: false,
  created_at: '2026-08-09T00:00:00Z',
})

const components = [
  component(11, 'breed', '绒云羊'),
  component(12, 'profession', '牧场学徒'),
  component(13, 'treasure', '棉花羊铃'),
]
const drawnComponents = Array.from({ length: 10 }, (_, index) => ({
  ...component(
    31 + index,
    'profession',
    index === 1 ? '羊羊侦探' : '牧场学徒',
    index === 1 ? 'reasoning' : 'code',
    index === 1 ? 9 : 7,
  ),
  position: index,
}))
const emptyCards = { items: [], total: 0, limit: 300, offset: 0 }
const emptyHistory = { items: [], total: 0, limit: 30, offset: 0 }
const emptyRedemptions = { items: [], total: 0, limit: 30, offset: 0 }
const rewardCard = {
  id: 101,
  position: 0,
  breed_id: 'code',
  breed_name: '绒云羊',
  profession_id: 'code',
  profession_name: '牧场学徒',
  treasure_id: 'code',
  treasure_name: '棉花羊铃',
  breed_component_id: 11,
  profession_component_id: 12,
  treasure_component_id: 13,
  recipe_id: 'code',
  title: '绒云咩',
  wool_value: 1000,
  is_jackpot: false,
  created_at: '2026-08-09T00:00:00Z',
}

let wrapper: VueWrapper | undefined

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: Boolean, title: String },
  emits: ['close'],
  template: `
    <section v-if="show" role="dialog" :aria-label="title">
      <button type="button" data-testid="close-dialog" @click="$emit('close')">关闭</button>
      <slot />
    </section>
  `,
})

function mountView() {
  wrapper = mount(RewardDrawView, {
    global: {
      plugins: [createPinia()],
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        BaseDialog: BaseDialogStub,
        Icon: true,
      },
    },
  })
  return wrapper
}

function mockReducedMotion(matches = false): void {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({
      matches,
      media: '(prefers-reduced-motion: reduce)',
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
}

describe('RewardDrawView three-pool composition flow', () => {
  beforeEach(() => {
    localStorage.clear()
    mockReducedMotion()
    getRewardDrawOverview.mockReset().mockResolvedValue(structuredClone(overview))
    getRewardDrawComponents.mockReset().mockResolvedValue({ items: components, total: 3, limit: 300, offset: 0 })
    getRewardDrawCards.mockReset().mockResolvedValue(emptyCards)
    getRewardDrawHistory.mockReset().mockResolvedValue(emptyHistory)
    getRewardDrawRedemptions.mockReset().mockResolvedValue(emptyRedemptions)
    combineRewardDrawComponents.mockReset().mockResolvedValue({
      cards: [{ ...rewardCard, wool_value: 32 }],
      quantity: 1,
      recipe_id: 'code',
      wool_total: 32,
    })
    redeemRewardDrawItems.mockReset()
    drawRewardCards.mockReset().mockResolvedValue({
      recovered: false,
      batch: {
        id: 1,
        request_key: 'draw-request',
        draw_count: 10,
        pool_version: 3,
        pool_id: 'profession',
        total_wool: 70,
        created_at: '2026-08-09T00:00:00Z',
        components: structuredClone(drawnComponents),
      },
    })
    Object.defineProperty(globalThis.crypto, 'randomUUID', {
      configurable: true,
      value: vi.fn(() => '00000000-0000-4000-8000-000000000001'),
    })
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    })
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.useRealTimers()
  })

  it('renders the selected arcade hierarchy without removed controls', async () => {
    const view = mountView()
    await flushPromises()

    expect(view.text()).toContain('咩咩召唤机')
    expect(view.text()).toContain('下一枚羊铃25%')
    expect(view.text()).not.toContain('整体奖励率约')
    expect(view.text()).not.toContain('概率公示')
    expect(view.text()).not.toContain('音效')
    expect(view.text()).not.toContain('升星')
    expect(view.findAll('.showcase-card')).toHaveLength(0)
    expect(view.text()).toContain('第一张组件卡会出现在这里')
    expect(view.get('[data-testid="draw-1"]').find('icon-stub[name="bell"]').exists()).toBe(true)
    expect(view.get('[data-testid="draw-10"]').find('icon-stub[name="bell"]').exists()).toBe(true)
    expect(view.get('[data-testid="draw-1"]').text()).not.toContain('$1')
    expect(view.get('[data-testid="draw-10"]').text()).not.toContain('$10')
  })

  it('sends the selected pool for ten-draw', async () => {
    const view = mountView()
    await flushPromises()

    await view.get('[data-testid="pool-profession"]').trigger('click')
    await view.get('[data-testid="draw-10"]').trigger('click')
    await flushPromises()

    expect(drawRewardCards).toHaveBeenCalledWith(10, 'profession', '00000000-0000-4000-8000-000000000001')
  })

  it('reveals the current batch before inventory and opens inventory only on request', async () => {
    vi.useFakeTimers()
    const view = mountView()
    await flushPromises()

    await view.get('[data-testid="pool-profession"]').trigger('click')
    await view.get('[data-testid="draw-10"]').trigger('click')
    await flushPromises()
    expect(view.find('[data-testid="draw-result-dialog"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(900)
    await flushPromises()

    expect(view.get('[role="dialog"]').attributes('aria-label')).toBe('召唤结算 · 10 张')
    expect(view.findAll('.result-card')).toHaveLength(10)
    expect(view.get('[data-testid="draw-result-dialog"]').text()).toContain('羊羊侦探')
    expect(view.find('.inventory-panel').exists()).toBe(false)

    await view.get('[data-testid="open-inventory-dialog"]').trigger('click')
    expect(view.get('[role="dialog"]').attributes('aria-label')).toBe('召唤结算 · 10 张')
    expect(view.find('.inventory-panel').exists()).toBe(false)

    await view.get('[data-testid="reveal-inventory"]').trigger('click')
    expect(view.get('[role="dialog"]').attributes('aria-label')).toBe('卡片图存')
    expect(view.find('.inventory-panel').exists()).toBe(true)
  })

  it('shows a single-card result after skipping and returns to the summon machine', async () => {
    drawRewardCards.mockResolvedValue({
      recovered: false,
      batch: {
        id: 2,
        request_key: 'single-draw-request',
        draw_count: 1,
        pool_version: 3,
        pool_id: 'breed',
        total_wool: 7,
        created_at: '2026-08-09T00:00:00Z',
        components: [{ ...component(51, 'breed', '绒云羊'), position: 0 }],
      },
    })
    const view = mountView()
    await flushPromises()

    await view.get('[data-testid="draw-1"]').trigger('click')
    await flushPromises()
    await view.get('.skip-button').trigger('click')

    expect(view.get('.result-grid').classes()).toContain('single')
    expect(view.get('[data-testid="draw-result-dialog"]').text()).toContain('绒云羊')
    expect(localStorage.getItem('sub2api:reward-draw:pending-v3')).toBeNull()

    await view.get('[data-testid="reveal-continue"]').trigger('click')
    expect(view.find('[role="dialog"]').exists()).toBe(false)
    expect(view.get('[data-testid="draw-1"]').attributes('disabled')).toBeUndefined()
  })

  it('restores a pending draw directly into results when reduced motion is enabled', async () => {
    mockReducedMotion(true)
    localStorage.setItem('sub2api:reward-draw:pending-v3', JSON.stringify({
      requestId: '00000000-0000-4000-8000-000000000001',
      count: 10,
      poolId: 'profession',
      userId: 7,
    }))

    const view = mountView()
    await flushPromises()
    await flushPromises()

    expect(drawRewardCards).toHaveBeenCalledWith(10, 'profession', '00000000-0000-4000-8000-000000000001')
    expect(view.get('[data-testid="draw-result-dialog"]').exists()).toBe(true)
    expect(view.find('.skip-button').exists()).toBe(false)
    expect(view.find('.inventory-panel').exists()).toBe(false)
  })

  it('caps the reveal stagger for a maximum-size all draw', async () => {
    const maximumComponents = Array.from({ length: 100 }, (_, index) => ({
      ...component(1000 + index, 'breed', '绒云羊'),
      position: index,
    }))
    getRewardDrawOverview.mockResolvedValue({
      ...overview,
      wallet: { ...overview.wallet, ticket_balance: 100 },
    })
    drawRewardCards.mockResolvedValue({
      recovered: false,
      batch: {
        id: 3,
        request_key: 'maximum-draw-request',
        draw_count: 100,
        pool_version: 3,
        pool_id: 'breed',
        total_wool: 700,
        created_at: '2026-08-09T00:00:00Z',
        components: maximumComponents,
      },
    })
    const view = mountView()
    await flushPromises()

    await view.get('[data-testid="draw-all"]').trigger('click')
    await flushPromises()
    await view.get('.skip-button').trigger('click')

    const resultCards = view.findAll('.result-card')
    expect(resultCards).toHaveLength(100)
    expect(resultCards.at(-1)?.attributes('style')).toContain('animation-delay: 540ms')
    expect(view.find('.inventory-panel').exists()).toBe(false)
  })

  it('opens all four tools as dialogs', async () => {
    const view = mountView()
    await flushPromises()

    const dialogs = [
      ['open-inventory-dialog', '卡片图存'],
      ['open-collection-dialog', '羊羊图鉴'],
      ['open-history-dialog', '召唤历史'],
      ['open-redeem-dialog', '兑换额度'],
    ] as const

    for (const [trigger, title] of dialogs) {
      await view.get(`[data-testid="${trigger}"]`).trigger('click')
      expect(view.get('[role="dialog"]').attributes('aria-label')).toBe(title)
      await view.get('[data-testid="close-dialog"]').trigger('click')
    }
  })

  it('defaults batch composition to the smallest selected stack', async () => {
    getRewardDrawComponents.mockResolvedValue({
      items: [
        component(11, 'breed', '绒云羊', 'code', 7, 8),
        component(12, 'profession', '牧场学徒', 'code', 7, 5),
        component(13, 'treasure', '棉花羊铃', 'code', 7, 12),
      ],
      total: 25,
      limit: 300,
      offset: 0,
    })
    combineRewardDrawComponents.mockResolvedValue({
      cards: Array.from({ length: 5 }, (_, index) => ({ ...rewardCard, id: 101 + index, wool_value: 32 })),
      quantity: 5,
      recipe_id: 'code',
      wool_total: 160,
    })
    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-inventory-dialog"]').trigger('click')

    await view.get('[data-testid="component-11"]').trigger('click')
    const tabs = view.findAll('.pool-tabs button')
    await tabs[1].trigger('click')
    await view.get('[data-testid="component-12"]').trigger('click')
    await tabs[2].trigger('click')
    await view.get('[data-testid="component-13"]').trigger('click')
    expect((view.get('[data-testid="combine-quantity"]').element as HTMLInputElement).value).toBe('5')
    expect(view.get('[data-testid="combine-components"]').text()).toContain('合成 5 张绒云咩')
    await view.get('[data-testid="combine-components"]').trigger('click')
    await flushPromises()

    expect(combineRewardDrawComponents).toHaveBeenCalledTimes(1)
    expect(combineRewardDrawComponents).toHaveBeenCalledWith([11, 12, 13], 5)
  })

  it('supports manual batch quantity within the smallest stack limit', async () => {
    getRewardDrawComponents.mockResolvedValue({
      items: components.map((item) => ({ ...item, stack_count: 4 })),
      total: 12,
      limit: 300,
      offset: 0,
    })
    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-inventory-dialog"]').trigger('click')
    await view.get('[data-testid="component-11"]').trigger('click')
    const tabs = view.findAll('.pool-tabs button')
    await tabs[1].trigger('click')
    await view.get('[data-testid="component-12"]').trigger('click')
    await tabs[2].trigger('click')
    await view.get('[data-testid="component-13"]').trigger('click')

    const quantityInput = view.get('[data-testid="combine-quantity"]')
    await quantityInput.setValue('0')
    expect((quantityInput.element as HTMLInputElement).value).toBe('1')
    await quantityInput.setValue('99')
    expect((quantityInput.element as HTMLInputElement).value).toBe('4')
    await quantityInput.setValue('2')
    await view.get('[data-testid="combine-components"]').trigger('click')
    await flushPromises()

    expect(combineRewardDrawComponents).toHaveBeenCalledWith([11, 12, 13], 2)
  })

  it('shows fixed recipes and disables a mixed normal-tier selection', async () => {
    getRewardDrawComponents.mockResolvedValue({
      items: [...components, component(14, 'profession', '羊羊侦探', 'reasoning', 9)],
      total: 4,
      limit: 300,
      offset: 0,
    })
    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-inventory-dialog"]').trigger('click')

    expect(view.text()).toContain('绒云羊 + 牧场学徒 + 棉花羊铃 → 绒云咩')
    await view.get('[data-testid="component-11"]').trigger('click')
    await view.findAll('.pool-tabs button')[1].trigger('click')
    expect(view.get('[data-testid="component-14"]').attributes('disabled')).toBeDefined()
  })

  it('stacks identical components and shows one fewer after composition', async () => {
    const stacked = [
      ...components.map((item) => ({ ...item, stack_count: 2 })),
      component(21, 'breed', '绒云羊', 'code', 7, 2),
      component(22, 'profession', '牧场学徒', 'code', 7, 2),
      component(23, 'treasure', '棉花羊铃', 'code', 7, 2),
    ]
    getRewardDrawComponents
      .mockReset()
      .mockResolvedValueOnce({ items: stacked, total: 6, limit: 300, offset: 0 })
      .mockResolvedValue({ items: components, total: 3, limit: 300, offset: 0 })

    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-inventory-dialog"]').trigger('click')
    expect(view.get('[data-testid="component-11"] .component-quantity').text()).toBe('×2')

    await view.get('[data-testid="component-11"]').trigger('click')
    const tabs = view.findAll('.pool-tabs button')
    await tabs[1].trigger('click')
    await view.get('[data-testid="component-12"]').trigger('click')
    await tabs[2].trigger('click')
    await view.get('[data-testid="component-13"]').trigger('click')
    await view.get('[data-testid="combine-components"]').trigger('click')
    await flushPromises()

    await view.get('[data-testid="inventory-components-tab"]').trigger('click')
    await view.findAll('.pool-tabs button')[0].trigger('click')
    expect(view.get('[data-testid="component-11"] .component-quantity').text()).toBe('×1')
  })

  it('shows combination cards in their own inventory tab', async () => {
    getRewardDrawCards.mockResolvedValue({ ...emptyCards, items: [rewardCard], total: 1 })

    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-inventory-dialog"]').trigger('click')
    await view.get('[data-testid="inventory-cards-tab"]').trigger('click')

    expect(view.findAll('.card-vault-card')).toHaveLength(1)
    expect(view.get('.card-vault-card').text()).toContain('绒云咩')
    expect(view.get('.card-vault-card img').attributes('src')).toBe('/reward-draw/generated/card-code.webp')
  })

  it('redeems one uncombined component below one dollar', async () => {
    getRewardDrawOverview.mockResolvedValue({
      ...overview,
      wallet: { ...overview.wallet, available_components: 1, available_wool: 7, available_reward: 0.007 },
    })
    getRewardDrawComponents.mockResolvedValue({ items: [components[0]], total: 1, limit: 300, offset: 0 })
    redeemRewardDrawItems.mockResolvedValue({
      id: 54,
      redeem_code_id: 76,
      redeem_code: 'single00000000000000000000000000',
      component_count: 1,
      card_count: 0,
      wool: 7,
      reward: 0.007,
      status: 'unused',
      created_at: '2026-08-09T01:02:03Z',
    })

    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-redeem-dialog"]').trigger('click')
    await view.get('[data-testid="redeem-maximum"]').setValue(false)
    await view.get('.finished-card input').setValue(true)
    await view.get('[data-testid="redeem-selected"]').trigger('click')
    await flushPromises()

    expect(redeemRewardDrawItems).toHaveBeenCalledWith([11], [], false)
  })

  it('defaults to the server-side maximum batch across both card types', async () => {
    const secondCard = { ...rewardCard, id: 102, title: '侦探咩', recipe_id: 'reasoning', wool_value: 41 }
    getRewardDrawOverview.mockResolvedValue({
      ...overview,
      wallet: { ...overview.wallet, available_components: 3, available_cards: 2, available_wool: 1094, available_reward: 1.094 },
    })
    getRewardDrawCards.mockResolvedValue({ ...emptyCards, items: [rewardCard, secondCard], total: 2 })
    redeemRewardDrawItems.mockResolvedValue({
      id: 56,
      redeem_code_id: 78,
      redeem_code: 'maximum000000000000000000000000',
      component_count: 3,
      card_count: 2,
      wool: 1094,
      reward: 1.094,
      status: 'unused',
      created_at: '2026-08-09T01:02:03Z',
    })

    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-redeem-dialog"]').trigger('click')

    expect((view.get('[data-testid="redeem-maximum"]').element as HTMLInputElement).checked).toBe(true)
    expect(view.get('.redeem-actions').text()).toContain('已选 5 张')
    await view.get('[data-testid="redeem-selected"]').trigger('click')
    await flushPromises()
    expect(redeemRewardDrawItems).toHaveBeenCalledWith([], [], true)
  })

  it('shows recoverable history and the newly generated redeem code', async () => {
    const previous = {
      id: 44,
      redeem_code_id: 66,
      redeem_code: 'previous000000000000000000000000',
      component_count: 0,
      card_count: 2,
      wool: 1000,
      reward: 1,
      status: 'unused',
      created_at: '2026-08-08T01:02:03Z',
    }
    getRewardDrawOverview.mockResolvedValue({
      ...overview,
      wallet: { ...overview.wallet, available_cards: 1, available_wool: 1000, available_reward: 1 },
    })
    getRewardDrawCards.mockResolvedValue({ ...emptyCards, items: [rewardCard], total: 1 })
    getRewardDrawRedemptions.mockResolvedValue({ items: [previous], total: 1, limit: 30, offset: 0 })
    redeemRewardDrawItems.mockResolvedValue({
      id: 55,
      redeem_code_id: 77,
      redeem_code: '0123456789abcdef0123456789abcdef',
      component_count: 0,
      card_count: 1,
      wool: 1000,
      reward: 1,
      status: 'unused',
      created_at: '2026-08-09T01:02:03Z',
    })

    const view = mountView()
    await flushPromises()
    await view.get('[data-testid="open-redeem-dialog"]').trigger('click')
    expect(view.text()).toContain(previous.redeem_code)

    await view.get('[data-testid="redeem-maximum"]').setValue(false)
    await view.get('.finished-card input').setValue(true)
    await view.get('[data-testid="redeem-selected"]').trigger('click')
    await flushPromises()

    expect(redeemRewardDrawItems).toHaveBeenCalledWith([], [101], false)
    expect(view.get('[data-testid="generated-redeem-code"]').text()).toBe('0123456789abcdef0123456789abcdef')
    await view.get('[data-testid="copy-redeem-code"]').trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('0123456789abcdef0123456789abcdef')
  })
})
