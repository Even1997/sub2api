<template>
  <div class="bill-table-scroll" tabindex="0" :aria-label="t('bills.metering')">
    <table class="bill-table bill-models">
      <thead><tr><th>{{ t('bills.model') }}</th><th>{{ t('bills.usage') }}</th><th>{{ t('bills.credit') }}</th></tr></thead>
      <tbody>
        <tr v-for="m in models" :key="m.model"><td>{{ m.model }}</td><td>{{ billUsage(m, t) }}</td><td :title="m.cost">${{ billMoney(m.cost, exact) }}</td></tr>
        <tr v-if="!models.length"><td colspan="3" class="bill-muted">{{ t('bills.noUsage') }}</td></tr>
      </tbody>
    </table>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { BillModel } from '@/api/monthlyBills'
import { billMoney, billUsage } from '@/utils/monthlyBill'
defineProps<{ models: BillModel[]; exact: boolean }>()
const { t } = useI18n()
</script>
