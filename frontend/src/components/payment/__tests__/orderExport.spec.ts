import { describe, expect, it } from 'vitest'
import { orderExportRange, ordersCSV } from '../orderExport'
import type { PaymentOrder } from '@/types/payment'

describe('order export', () => {
  it('includes the full last local day and rejects missing or inverted dates', () => {
    const range = orderExportRange('2026-10-01', '2026-10-02')!
    expect(new Date(range.start_time)).toEqual(new Date(2026, 9, 1))
    expect(new Date(range.end_time)).toEqual(new Date(2026, 9, 3))
    expect(orderExportRange('', '2026-10-02')).toBeNull()
    expect(orderExportRange('2026-10-03', '2026-10-02')).toBeNull()
    expect(orderExportRange('2026-10-02', '2026-10-02')).not.toBeNull()
  })

  it('escapes multiline CSV fields, protects formulas, and preserves currency and decimals', () => {
    const order = { id: 1, out_trade_no: 'order-1', pay_amount: 100, amount: 14,
      currency: 'CNY', payment_type: 'alipay', status: 'PARTIALLY_REFUNDED', created_at: '2026-10-01T00:00:00Z',
    } as PaymentOrder
    for (const remark of ['=1+1', ' +1', '-1', '@SUM(A1)', '\t=1', '\n=1']) {
      expect(ordersCSV([{ ...order, remark }], key => key)).toContain(`"'${remark}"`)
    }
    const csv = ordersCSV([{ ...order, remark: '中文, "备注"\n第二行' }], key => key)
    expect(csv.startsWith('\uFEFF')).toBe(true)
    expect(csv).toContain('"中文, ""备注""\n第二行"')
    expect(csv).toContain('"100.00","CNY","14.00"')
    expect(csv).toContain('payment.status.partially_refunded')
  })
})
