import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/admin/account/ReAuthAccountModal.vue'),
  'utf8'
)

describe('ReAuthAccountModal OpenAI protocol re-login', () => {
  it('provides saved mailbox configuration and background re-login actions', () => {
    expect(source).toContain("t('admin.accounts.openai.reauth.addConfig')")
    expect(source).toContain('saveOpenAIOAuthReauthConfig')
    expect(source).toContain('startOpenAIOAuthReauth')
    expect(source).toContain('type="email"')
    expect(source).toContain('type="url"')
  })

  it('polls active tasks and stops polling when the modal closes or unmounts', () => {
    expect(source).toContain('openaiReauthTaskActive')
    expect(source).toContain('setInterval(() =>')
    expect(source).toContain('stopOpenAIReauthPolling()')
    expect(source).toContain("t('admin.accounts.openai.reauth.retired')")
    expect(source).toContain('account_retired')
    expect(source).toContain('onBeforeUnmount(stopOpenAIReauthPolling)')
  })

  it('keeps the automatic flow protocol-only', () => {
    expect(source).not.toMatch(/Roxy|Selenium|Playwright|WebDriver/)
  })

  it('adds account-bound manual BPS authorization without replacing Codex credentials', () => {
    expect(source).toContain('showManualBPS')
    expect(source).toContain('beginExcelManualAuthorization')
    expect(source).toContain('completeExcelManualAuthorization')
    expect(source).toContain('extractExcelCallback')
    expect(source).toContain("t('admin.accounts.openai.manualBps.title')")
    expect(source).toContain('completeExcelManualAuthorization')
    expect(source).not.toContain('applyOAuthCredentials(props.account.id, {\n        callback')
  })
})
