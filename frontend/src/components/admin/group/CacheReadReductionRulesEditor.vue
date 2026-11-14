<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { CacheReadReductionRule } from '@/types'

const props = defineProps<{ modelValue: CacheReadReductionRule[] }>()
const emit = defineEmits<{ 'update:modelValue': [value: CacheReadReductionRule[]] }>()
const { t } = useI18n()
const key = 'admin.groups.form.cacheReadRules.'

function update(index: number, patch: Partial<CacheReadReductionRule>) {
  emit('update:modelValue', props.modelValue.map((rule, i) => i === index ? { ...rule, ...patch } : rule))
}

function setMode(index: number, event: Event) {
  update(index, { start_hit_rate_percent: (event.target as HTMLSelectElement).value === 'dynamic' ? 80 : null })
}

function preview(rule: CacheReadReductionRule) {
  const start = rule.start_hit_rate_percent
  if (!Number.isFinite(rule.percent) || (start != null && (!Number.isFinite(start) || start < 0 || start >= 100))) return '—'
  return (start == null ? rule.percent : rule.percent * Math.max(0, (96 - start) / (100 - start))).toFixed(2)
}
</script>

<template>
  <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" :aria-label="t(key + 'title')">
    <div class="flex items-center justify-between gap-3">
      <h4 class="text-sm font-medium">{{ t(key + 'title') }}</h4>
      <button type="button" class="btn btn-secondary inline-flex items-center gap-1" :disabled="modelValue.length >= 200"
        @click="emit('update:modelValue', [...modelValue, { model: '', percent: 5, start_hit_rate_percent: 80 }])">
        <Icon name="plus" size="sm" />{{ t(key + 'add') }}
      </button>
    </div>
    <p class="input-hint">{{ t(key + 'inherit') }}</p>
    <div v-for="(rule, index) in modelValue" :key="index" class="grid min-w-0 grid-cols-1 gap-3 border-b border-gray-100 pb-3 sm:grid-cols-2 dark:border-dark-700">
      <label class="min-w-0">
        <span class="input-label">{{ t(key + 'model') }}</span>
        <input class="input w-full" :value="rule.model" required maxlength="200" pattern="[^\s*?]+" placeholder="gpt-6.1-sol"
          @input="update(index, { model: ($event.target as HTMLInputElement).value })" />
      </label>
      <label>
        <span class="input-label">{{ t(key + 'mode') }}</span>
        <select class="input" :value="rule.start_hit_rate_percent == null ? 'fixed' : 'dynamic'" @change="setMode(index, $event)">
          <option value="fixed">{{ t(key + 'fixed') }}</option>
          <option value="dynamic">{{ t(key + 'dynamic') }}</option>
        </select>
      </label>
      <label>
        <span class="input-label">{{ t(key + (rule.start_hit_rate_percent == null ? 'percent' : 'maximum')) }}</span>
        <input class="input" type="number" min="0" max="100" step="0.1" required :value="rule.percent"
          @input="update(index, { percent: ($event.target as HTMLInputElement).valueAsNumber })" />
      </label>
      <label v-if="rule.start_hit_rate_percent != null">
        <span class="input-label">{{ t(key + 'start') }}</span>
        <input class="input" type="number" min="0" max="99.9" step="0.1" required :value="rule.start_hit_rate_percent"
          @input="update(index, { start_hit_rate_percent: ($event.target as HTMLInputElement).valueAsNumber })" />
      </label>
      <div class="flex min-w-0 items-center justify-between gap-2 sm:col-span-2">
        <span class="text-xs text-gray-500">{{ t(key + 'preview', { percent: preview(rule) }) }}</span>
        <button type="button" class="flex h-8 w-8 shrink-0 items-center justify-center rounded text-gray-500 hover:text-red-600"
          :title="t(key + 'remove')" :aria-label="t(key + 'remove')" @click="emit('update:modelValue', modelValue.filter((_, i) => i !== index))">
          <Icon name="trash" size="sm" />
        </button>
      </div>
    </div>
  </section>
</template>
