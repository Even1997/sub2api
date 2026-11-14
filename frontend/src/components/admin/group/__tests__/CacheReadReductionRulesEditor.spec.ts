import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import CacheReadReductionRulesEditor from '../CacheReadReductionRulesEditor.vue'
import zh from '@/i18n/locales/zh/admin/overview'
import type { CacheReadReductionRule } from '@/types'

function editor(rules: CacheReadReductionRule[] = []) {
  return mount(CacheReadReductionRulesEditor, {
    props: { modelValue: rules },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { admin: zh } } })] },
  })
}

describe('cache-read model overrides', () => {
  it('adds a dynamic rule, switches to fixed and removes the override', async () => {
    const wrapper = editor()
    await wrapper.find('button').trigger('click')
    const added = wrapper.emitted('update:modelValue')![0][0] as CacheReadReductionRule[]
    expect(added).toEqual([{ model: '', percent: 5, start_hit_rate_percent: 80 }])
    await wrapper.setProps({ modelValue: [{ ...added[0], model: 'gpt-6.1-sol' }] })
    expect(wrapper.text()).toContain('4.00%')
    await wrapper.find('select').setValue('fixed')
    const fixed = wrapper.emitted('update:modelValue')!.at(-1)![0] as CacheReadReductionRule[]
    expect(fixed[0].start_hit_rate_percent).toBeNull()
    await wrapper.setProps({ modelValue: fixed })
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(1)
    await wrapper.find('button[aria-label="删除模型覆盖"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')!.at(-1)![0]).toEqual([])
  })

  it('edits one rule without mutating a group snapshot or sibling rules', async () => {
    const original = [{ model: 'model-a', percent: 5 }, { model: 'model-b', percent: 0 }]
    const wrapper = editor(original)
    await wrapper.find('input[type="number"]').setValue('12.5')
    const updated = wrapper.emitted('update:modelValue')!.at(-1)![0] as CacheReadReductionRule[]
    expect(original[0].percent).toBe(5)
    expect(updated[0].percent).toBe(12.5)
    expect(updated[1]).toEqual(original[1])
    expect(wrapper.find('input[type="number"]').attributes()).toMatchObject({ min: '0', max: '100' })
  })
})
