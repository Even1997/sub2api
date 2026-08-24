import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const routerSource = readFileSync(resolve(dir, '../index.ts'), 'utf8')
const sidebarSource = readFileSync(resolve(dir, '../../components/layout/AppSidebar.vue'), 'utf8')
const pageSource = readFileSync(resolve(dir, '../../views/user/RewardDrawView.vue'), 'utf8')
const apiSource = readFileSync(resolve(dir, '../../api/rewardDraw.ts'), 'utf8')

describe('reward draw user experience', () => {
  it('registers the authenticated reward route without the retired feature', () => {
    expect(routerSource).toContain("path: '/reward-draw'")
    expect(routerSource).toContain("import('@/views/user/RewardDrawView.vue')")
    expect(routerSource).not.toContain("path: '/compute-cottage'")
  })

  it('uses the reward draw entry in the user sidebar', () => {
    expect(sidebarSource).toContain("path: '/reward-draw', label: t('nav.rewardDraw')")
    expect(sidebarSource).not.toContain("path: '/compute-cottage', label: t('nav.computeCottage')")
  })

  it('includes batch draw, redemption, recovery, history, and pool surfaces', () => {
    expect(pageSource).toContain('data-testid="draw-10"')
    expect(pageSource).toContain('data-testid="draw-all"')
    expect(pageSource).toContain('PENDING_DRAW_KEY')
    expect(pageSource).toContain('data-testid="open-history-dialog"')
    expect(pageSource).toContain('data-testid="open-redeem-dialog"')
    expect(pageSource).toContain('pool-selector')
    expect(pageSource).toContain('prefers-reduced-motion: reduce')
    expect(apiSource).toContain("'/reward-draw/draw'")
    expect(apiSource).toContain("'/reward-draw/redeem'")
  })
})
