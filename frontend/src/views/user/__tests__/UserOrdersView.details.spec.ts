import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserOrdersView from '../UserOrdersView.vue'

const mocks = vi.hoisted(() => ({
  getMyOrders: vi.fn(), getRefundEligibleProviders: vi.fn(), updateOrderRemark: vi.fn(),
  exportMyOrders: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(), saveAs: vi.fn(),
}))
vi.mock('@/api/payment', () => ({ paymentAPI: mocks }))
vi.mock('@/stores', () => ({ useAppStore: () => mocks }))
vi.mock('file-saver', () => ({ saveAs: mocks.saveAs }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (original) => ({
  ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.resetAllMocks()
  mocks.getMyOrders.mockResolvedValue({ data: { items: [{
    id: 42, remark: '十月报销', status: 'COMPLETED', amount: 10, pay_amount: 70,
    currency: 'CNY', out_trade_no: 'order-42', payment_type: 'alipay', created_at: '2026-10-01T01:00:00Z',
  }], total: 1 } })
  mocks.getRefundEligibleProviders.mockResolvedValue({ data: { provider_instance_ids: [] } })
})

async function openOrders() {
  const wrapper = mount(UserOrdersView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, Icon: true, Pagination: true, teleport: true,
  } } })
  await flushPromises()
  return wrapper
}

describe('order details actions', () => {
  it('shows remarks, pre-fills the dialog, saves edits and allows clearing', async () => {
    const wrapper = await openOrders()
    expect(wrapper.text()).toContain('十月报销')
    await wrapper.get('[data-test="edit-remark"]').trigger('click')
    expect((wrapper.get('#order-remark').element as HTMLTextAreaElement).value).toBe('十月报销')
    await wrapper.get('#order-remark').setValue('新的备注')
    mocks.updateOrderRemark.mockResolvedValue({ data: { remark: '新的备注' } })
    await wrapper.get('[data-test="save-remark"]').trigger('click')
    await flushPromises()
    expect(mocks.updateOrderRemark).toHaveBeenCalledWith(42, '新的备注')
    expect(wrapper.text()).toContain('新的备注')
    await wrapper.get('[data-test="edit-remark"]').trigger('click')
    await wrapper.get('#order-remark').setValue('')
    mocks.updateOrderRemark.mockResolvedValue({ data: { remark: '' } })
    await wrapper.get('[data-test="save-remark"]').trigger('click')
    await flushPromises()
    expect(mocks.updateOrderRemark).toHaveBeenLastCalledWith(42, '')
    expect(wrapper.find('#order-remark').exists()).toBe(false)
  })

  it('keeps the draft after a failed save and prevents duplicate submission', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[data-test="edit-remark"]').trigger('click')
    await wrapper.get('#order-remark').setValue('保留草稿')
    let rejectSave!: (reason: Error) => void
    mocks.updateOrderRemark.mockReturnValue(new Promise((_, reject) => { rejectSave = reject }))
    await wrapper.get('[data-test="save-remark"]').trigger('click')
    expect(wrapper.get('[data-test="save-remark"]').attributes('disabled')).toBeDefined()
    rejectSave(new Error('failed'))
    await flushPromises()
    expect((wrapper.get('#order-remark').element as HTMLTextAreaElement).value).toBe('保留草稿')
    expect(mocks.showError).toHaveBeenCalled()
  })

  it('validates dates and exports the full API result including other pages', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[data-test="export-orders"]').trigger('click')
    await wrapper.get('#order-export-start').setValue('2026-10-02')
    await wrapper.get('#order-export-end').setValue('2026-10-01')
    expect(wrapper.get('[data-test="confirm-export"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#order-export-end').setValue('2026-10-03')
    const items = Array.from({ length: 25 }, (_, id) => ({
      id, out_trade_no: `export-${id}`, pay_amount: 10, amount: 10, status: 'COMPLETED',
      payment_type: 'alipay', created_at: '2026-10-02T00:00:00Z', remark: '导出备注',
    }))
    mocks.exportMyOrders.mockResolvedValue({ data: items })
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    await flushPromises()
    expect(mocks.exportMyOrders).toHaveBeenCalledWith({
      start_time: new Date(2026, 9, 2).toISOString(), end_time: new Date(2026, 9, 4).toISOString(), status: undefined,
    })
    expect(mocks.saveAs).toHaveBeenCalledWith(expect.any(Blob), 'orders_2026-10-02_2026-10-03.csv')
    const reader = new FileReader()
    const content = await new Promise<string>(resolve => {
      reader.onload = () => resolve(reader.result as string)
      reader.readAsText(mocks.saveAs.mock.calls[0][0])
    })
    expect(content).toContain('export-24')
    expect(content).toContain('导出备注')
  })

  it('keeps the export dialog open and downloads nothing for empty results or errors', async () => {
    const wrapper = await openOrders()
    await wrapper.get('[data-test="export-orders"]').trigger('click')
    mocks.exportMyOrders.mockResolvedValue({ data: [] })
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    await flushPromises()
    expect(mocks.saveAs).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('payment.orders.noExportOrders')
    mocks.exportMyOrders.mockRejectedValue({ reason: 'ORDER_EXPORT_TOO_LARGE' })
    await wrapper.get('[data-test="confirm-export"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('#order-export-start').exists()).toBe(true)
    expect(mocks.saveAs).not.toHaveBeenCalled()
  })
})
