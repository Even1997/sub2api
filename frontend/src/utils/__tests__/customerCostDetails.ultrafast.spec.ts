import { describe, expect, it } from 'vitest'
import type { UsageLog } from '@/types'
import { customerCostSnapshot, customerCostsUnknown } from '../customerCostDetails'

const fixture = (): UsageLog => ({
 service_tier:'ultrafast',rate_multiplier:.6,actual_cost:.000996,total_cost:.00996,
 input_cost:.00426,output_cost:.0057,cache_creation_cost:0,cache_read_cost:0,
 customer_cost_details:{version:1,free_fast:false,free_ultrafast:true,input_cost:.00071,output_cost:.00095,
 image_input_cost:0,image_output_cost:0,cache_creation_cost:0,cache_read_cost:0,total_cost:.00166,actual_cost:.000996,multiplier:.6},
} as UsageLog)
describe('UltraFast customer cost snapshots',()=>{
 it('uses the saved Standard quote while retaining sixfold upstream costs',()=>{
  const row=fixture();expect(customerCostSnapshot(row)?.input_cost).toBe(.00071)
  expect(row.total_cost).toBe(.00996);expect(customerCostsUnknown(row)).toBe(false)
 })
 it('does not accept a Free Fast marker as a Free UltraFast snapshot',()=>{
  const row=fixture();row.customer_cost_details!.free_ultrafast=false;row.customer_cost_details!.free_fast=true
  expect(customerCostSnapshot(row)).toBeNull();expect(customerCostsUnknown(row)).toBe(true)
 })
 it('does not reconstruct discounted prices without a snapshot',()=>{
  const row=fixture();row.customer_cost_details=null
  expect(customerCostSnapshot(row)).toBeNull();expect(customerCostsUnknown(row)).toBe(true)
 })
 it('rejects a snapshot inconsistent with actual billing',()=>{
  const row=fixture();row.actual_cost=.005976
  expect(customerCostSnapshot(row)).toBeNull()
 })
})


it('uses the saved probe penalty quote for fast and standard requests', () => {
  for (const service_tier of [null, 'fast']) {
    const row = fixture()
    row.service_tier = service_tier
    row.actual_cost = .01
    row.customer_cost_details = {
      version: 4, free_fast: false, input_cost: .00071, output_cost: .00095,
      image_input_cost: 0, image_output_cost: 0, cache_creation_cost: 0, cache_read_cost: 0,
      total_cost: .00166, actual_cost: .01, multiplier: .6,
      probe_penalty: { minimum_charge: .01, adjustment: .009004, actual_cost: .01, original_output_tokens: 19, billing_output_tokens: 200, output_token_price: .00005 }
    }
    expect(customerCostSnapshot(row)).toBe(row.customer_cost_details)
    expect(customerCostsUnknown(row)).toBe(false)
    row.customer_cost_details.probe_penalty!.adjustment = .009
    expect(customerCostSnapshot(row)).toBeNull()
  }
})
