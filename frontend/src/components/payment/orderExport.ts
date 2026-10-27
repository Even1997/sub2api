import type { PaymentOrder } from '@/types/payment'

export function orderExportRange(startDate: string, endDate: string) {
  const start = new Date(`${startDate}T00:00:00`)
  const end = new Date(`${endDate}T00:00:00`)
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || start > end) return null
  end.setDate(end.getDate() + 1)
  return { start_time: start.toISOString(), end_time: end.toISOString() }
}

export function ordersCSV(orders: PaymentOrder[], t: (key: string) => string): string {
  const headers = ['orderId', 'orderNo', 'payAmount', 'currency', 'creditedAmountUsd', 'paymentMethod', 'status', 'createdAt', 'remark']
    .map(key => t(`payment.orders.${key}`))
  const rows = orders.map(order => [
    order.id, order.out_trade_no, order.pay_amount.toFixed(2), order.currency || 'CNY',
    order.amount.toFixed(2), t(`payment.methods.${order.payment_type}`),
    t(`payment.status.${order.status.toLowerCase()}`),
    new Date(order.created_at).toLocaleString(), order.remark || '',
  ])
  return '\uFEFF' + [headers, ...rows].map(row => row.map(value => {
    let text = String(value)
    // Treat user-controlled cells as text, including formulas after whitespace.
    if (/^[\s\uFEFF]*[=+\-@]/u.test(text) || /^[\t\r\n]/u.test(text)) text = "'" + text
    return '"' + text.replace(/"/g, '""') + '"'
  }).join(',')).join('\r\n')
}
