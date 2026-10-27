import type { BillModel } from '@/api/monthlyBills'

// Round display only, without converting exact ledger amounts to binary floats.
export function billMoney(value: string, exact = false): string {
  if (exact) return value
  const negative = value.startsWith('-')
  const [integer, fraction = ''] = value.replace(/^-/, '').split('.')
  let cents = BigInt(integer || '0') * 100n + BigInt((fraction + '00').slice(0, 2))
  if (Number(fraction[2] || '0') >= 5) cents++
  return `${negative && cents > 0n ? '-' : ''}${(cents / 100n).toLocaleString('en-US')}.${String(cents % 100n).padStart(2, '0')}`
}

export function billUsage(m: BillModel, t: (key: string) => string): string {
  return (['requests', 'tokens', 'images', 'video_seconds', 'videos'] as const)
    .filter(key => BigInt(m[key]) !== 0n)
    .map(key => `${BigInt(m[key]).toLocaleString('en-US')} ${t(`bills.units.${key}`)}`).join(' · ') || '—'
}

export function shanghaiMonth(now = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit' }).formatToParts(now)
  return `${parts.find(p => p.type === 'year')!.value}-${parts.find(p => p.type === 'month')!.value}`
}
export function previousMonth(month: string): string {
  const [year, m] = month.split('-').map(Number)
  return `${m === 1 ? year! - 1 : year}-${String(m === 1 ? 12 : m! - 1).padStart(2, '0')}`
}
