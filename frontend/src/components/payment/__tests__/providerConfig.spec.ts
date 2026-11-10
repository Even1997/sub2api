import { describe, expect, it } from 'vitest'
import {
  PAYMENT_CURRENCY_OPTIONS,
  PROVIDER_CONFIG_FIELDS,
  getAvailableTypes,
  getDefaultSupportedTypes,
  isBuiltInAlipayMethod,
  isBuiltInWxpayMethod,
  parseEasyPayCustomMethods,
  serializeEasyPayCustomMethods,
  parseCloudcatProducts,
  serializeCloudcatProducts,
  validateCloudcatProducts,
} from '@/components/payment/providerConfig'

function findField(providerKey: string, key: string) {
  const fields = PROVIDER_CONFIG_FIELDS[providerKey] || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('wxpay', 'publicKeyId')?.optional).toBeFalsy()
    expect(findField('wxpay', 'certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('wxpay', 'mpAppId')).toBeUndefined()
    expect(findField('wxpay', 'h5AppName')).toBeUndefined()
    expect(findField('wxpay', 'h5AppUrl')).toBeUndefined()
  })
})

describe('PROVIDER_CONFIG_FIELDS.airwallex', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('airwallex', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })

  it('marks accountId as optional and explains when it can be left blank', () => {
    const accountId = findField('airwallex', 'accountId')

    expect(accountId?.optional).toBe(true)
    expect(accountId?.clearable).toBe(true)
    expect(accountId?.hintKey).toBe('admin.settings.payment.field_accountIdHint')
  })

  it('explains that apiBase must match the Airwallex key environment', () => {
    expect(findField('airwallex', 'apiBase')?.hintKey).toBe('admin.settings.payment.field_airwallexApiBaseHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.stripe', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('stripe', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })
})

describe('EasyPay custom methods config', () => {
  it('parses customMethods from the JSON string stored in provider config', () => {
    expect(parseEasyPayCustomMethods(
      '[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":"USDT-TRC20"}]',
    )).toEqual([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: 'USDT-TRC20' },
    ])
  })

  it('serializes non-empty custom methods into the config string format', () => {
    expect(serializeEasyPayCustomMethods([
      { type: 'ldc', upstreamType: 'epay', displayName: 'LDC' },
      { type: '  ', upstreamType: 'ignored', displayName: 'Ignored' },
      { type: 'usdt_trc20', upstreamType: 'usdt', displayName: '' },
    ])).toBe('[{"type":"ldc","upstreamType":"epay","displayName":"LDC"},{"type":"usdt_trc20","upstreamType":"usdt","displayName":""}]')
  })

  it('returns an empty string for invalid or empty custom methods', () => {
    expect(parseEasyPayCustomMethods('not-json')).toEqual([])
    expect(serializeEasyPayCustomMethods([{ type: '', upstreamType: 'epay', displayName: 'LDC' }])).toBe('')
  })
})

describe('built-in payment method helpers', () => {
  it('only treats exact built-in aliases as Alipay or WeChat Pay', () => {
    expect(isBuiltInAlipayMethod('alipay')).toBe(true)
    expect(isBuiltInAlipayMethod('alipay_direct')).toBe(true)
    expect(isBuiltInAlipayMethod('card_alipay')).toBe(false)

    expect(isBuiltInWxpayMethod('wxpay')).toBe(true)
    expect(isBuiltInWxpayMethod('wxpay_direct')).toBe(true)
    expect(isBuiltInWxpayMethod('card_wxpay')).toBe(false)
  })
})

describe('Cloudcat product mapping', () => {
  it('supports one Alipay or WeChat channel and defaults to Alipay', () => {
    expect(getDefaultSupportedTypes('cloudcat')).toEqual(['alipay'])
    expect(getAvailableTypes('cloudcat', [
      { value: 'alipay', label: 'Alipay' },
      { value: 'wxpay', label: 'WeChat Pay' },
    ], 'Redirect')).toEqual([
      { value: 'alipay', label: 'Alipay' },
      { value: 'wxpay', label: 'WeChat Pay' },
    ])
  })

  it('parses, validates, and serializes amount-to-product rows', () => {
    const rows = parseCloudcatProducts('[{"amount":"5","itemUrl":"https://catfk.com/item/ee9c69"}]')

    expect(rows).toEqual([{ amount: '5', itemUrl: 'https://catfk.com/item/ee9c69' }])
    expect(validateCloudcatProducts(rows)).toBeNull()
    expect(serializeCloudcatProducts(rows)).toBe('[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]')
  })

  it('rejects duplicate amounts and untrusted product URLs', () => {
    expect(validateCloudcatProducts([
      { amount: '5', itemUrl: 'https://catfk.com/item/first' },
      { amount: '5.00', itemUrl: 'https://catfk.com/item/second' },
    ])).toBe('duplicate')
    expect(validateCloudcatProducts([
      { amount: '5', itemUrl: 'https://example.com/item/ee9c69' },
    ])).toBe('url')
  })
})
