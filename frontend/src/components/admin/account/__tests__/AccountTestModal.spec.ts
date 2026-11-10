import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, testAccount, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  testAccount: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels,
      testAccount
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        if (key === 'admin.accounts.imagePreviewAlt' && params?.index) {
          return `test-image-${params.index}`
        }
        return messages[key] || key
      }
    })
  }
})

function mountModal(account: Record<string, unknown> = {
  id: 42,
  name: 'Gemini Image Test',
  platform: 'gemini',
  type: 'apikey',
  status: 'active'
}) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: { template: '<div class="select-stub"></div>' },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' },
      { id: 'gemini-2.5-flash-image', display_name: 'Gemini 2.5 Flash Image' },
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini 3.1 Flash Image' }
    ])
    testAccount.mockReset()
    testAccount.mockImplementation(async (_id, request, options) => {
      options.onEvent({ type: 'test_start', model: request.model_id })
      if (String(request.model_id).includes('image')) {
        options.onEvent({ type: 'image', image_url: 'data:image/png;base64,QUJD', mime_type: 'image/png' })
      }
      options.onEvent({ type: 'test_complete', success: true })
      return { success: true, model: request.model_id }
    })
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('gemini 图片模型测试会携带提示词并渲染图片预览', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const promptInput = wrapper.find('textarea.textarea-stub')
    expect(promptInput.exists()).toBe(true)
    await promptInput.setValue('draw a tiny orange cat astronaut')

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(testAccount).toHaveBeenCalledTimes(1)
    expect(testAccount.mock.calls[0][1]).toEqual({
      model_id: 'gemini-3.1-flash-image',
      prompt: 'draw a tiny orange cat astronaut'
    })

    const preview = wrapper.find('img[alt="test-image-1"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('src')).toBe('data:image/png;base64,QUJD')
  })

  it('grok 账号测试默认选择 Grok 模型', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'grok-4.3', display_name: 'Grok 4.3' },
      { id: 'grok-build-0.1', display_name: 'Grok Build 0.1' }
    ])
    const wrapper = mountModal({
      id: 13,
      name: 'Grok Account',
      platform: 'grok',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()

    expect(testAccount).toHaveBeenCalledTimes(1)
    expect(testAccount.mock.calls[0][1]).toEqual({
      model_id: 'grok-4.3',
      prompt: '',
      mode: 'text'
    })
  })

  it('OpenAI Compact 探测会携带 compact 测试模式', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(testAccount).toHaveBeenCalledTimes(1)
    expect(testAccount.mock.calls[0][1]).toMatchObject({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'compact'
    })
  })
})


describe('Excel authorization model errors', () => {
  it('shows the authorization state, blocks an empty test and allows reloading', async () => {
    getAvailableModels.mockReset()
    getAvailableModels.mockRejectedValueOnce({reason: 'OPENAI_EXCEL_AUTH_VERIFICATION_REQUIRED', message: 'private error must not be rendered'})
    const wrapper = mountModal()
    await wrapper.setProps({show: true})
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.accounts.excelAuthErrors.OPENAI_EXCEL_AUTH_VERIFICATION_REQUIRED')
    expect(wrapper.text()).not.toContain('private error')
    expect(wrapper.get('[role="alert"] a').attributes('href')).toBe('/admin/token-guard-v2')
    const start = wrapper.findAll('button').find(b => b.text().includes('admin.accounts.startTest'))!
    expect(start.attributes('disabled')).toBeDefined()
    getAvailableModels.mockResolvedValueOnce([{id:'gpt-6-astra',display_name:'Astra'}])
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(start.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})


it('does not show legacy BPS authorization progress for managed authorization connections', async () => {
  getAvailableModels.mockResolvedValue([{id:'gpt-6-sol',display_name:'Sol'}])
  const wrapper = mountModal({id:42,name:'Managed BPS',platform:'openai',type:'oauth',status:'active',extra:{openai_excel_bps:true,openai_excel_bps_authorization_pending:true,openai_connections_managed:true}})
  await wrapper.setProps({show:true})
  await flushPromises()
  expect(wrapper.text()).not.toContain('admin.accounts.bpsAuthorizing')
  wrapper.unmount()
})

it('keeps native model testing available during automatic BPS authorization', async () => {
  getAvailableModels.mockResolvedValue([{id:'gpt-6-sol',display_name:'Sol'}])
  const wrapper = mountModal({id:42,name:'Automatic BPS',platform:'openai',type:'oauth',status:'active',extra:{openai_excel_bps:true,openai_excel_bps_authorization_pending:true}})
  await wrapper.setProps({show:true})
  await flushPromises()
  expect(wrapper.get('[role="status"]').text()).toContain('admin.accounts.bpsAuthorizing')
  const start=wrapper.findAll('button').find(b=>b.text().includes('admin.accounts.startTest'))!
  expect(start.attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
