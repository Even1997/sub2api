<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- Filters -->
      <div class="card p-4">
        <div class="flex flex-wrap items-center gap-3">
          <Select v-model="currentFilter" :options="statusFilters" class="w-36" @change="handlePageChange(1)" />
          <div class="flex flex-1 items-center justify-end gap-2">
            <button class="btn btn-secondary" data-test="export-orders" @click="showExport = true">
              <Icon name="download" size="md" />{{ t('payment.orders.export') }}
            </button>
            <button @click="fetchOrders" :disabled="loading" class="btn btn-secondary" :title="t('common.refresh')">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button class="btn btn-primary" @click="router.push('/purchase')">{{ t('payment.result.backToRecharge') }}</button>
          </div>
        </div>
      </div>

      <!-- Table -->
      <OrderTable :orders="orders" :loading="loading">
        <template #actions="{ row }">
          <div class="flex items-center gap-2">
            <button class="inline-flex min-h-11 items-center whitespace-nowrap rounded-md px-3 py-2 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-300 dark:hover:bg-primary-900/20" data-test="edit-remark" @click="openRemarkDialog(row)">
              {{ t('payment.orders.editRemark') }}
            </button>
            <button
              v-if="row.status === 'PENDING' && row.resumable"
              data-test="resume-order"
              :disabled="resumingOrderId === row.id"
              class="inline-flex min-h-11 items-center gap-1 rounded-md px-3 py-2 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-300 dark:hover:bg-primary-900/20"
              @click="handleResume(row)"
            >
              <Icon name="externalLink" size="sm" />
              <span>{{ resumingOrderId === row.id ? t('common.processing') : t('payment.qr.continuePayment') }}</span>
            </button>
            <button v-if="row.status === 'PENDING'" @click="handleCancel(row)" class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-yellow-600 hover:bg-yellow-50 dark:text-yellow-400 dark:hover:bg-yellow-900/20">
              <Icon name="x" size="sm" />
              <span>{{ t('payment.orders.cancel') }}</span>
            </button>
            <button v-if="canRequestRefund(row)" @click="openRefundDialog(row)" class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-purple-600 hover:bg-purple-50 dark:text-purple-400 dark:hover:bg-purple-900/20">
              <Icon name="dollar" size="sm" />
              <span>{{ t('payment.orders.requestRefund') }}</span>
            </button>
          </div>
        </template>
      </OrderTable>

      <!-- Pagination -->
      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </div>

    <BaseDialog :show="!!remarkTarget" :title="t('payment.orders.editRemark')" :close-on-escape="!savingRemark" :show-close-button="!savingRemark" @close="remarkTarget = null">
      <label for="order-remark" class="input-label">{{ t('payment.orders.remark') }} · #{{ remarkTarget?.id }}</label>
      <textarea id="order-remark" v-model="remarkDraft" rows="4" maxlength="500" class="input mt-2 w-full" :disabled="savingRemark" :placeholder="t('payment.orders.remarkPlaceholder')" aria-describedby="order-remark-help" />
      <p id="order-remark-help" class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.orders.remarkHint') }} · {{ remarkDraft.length }}/500</p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" :disabled="savingRemark" @click="remarkTarget = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" data-test="save-remark" :disabled="savingRemark" @click="saveRemark">{{ savingRemark ? t('common.processing') : t('common.save') }}</button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="showExport" :title="t('payment.orders.export')" :close-on-escape="!exporting" :show-close-button="!exporting" @close="showExport = false">
      <p class="mb-4 text-sm text-gray-600 dark:text-gray-300">{{ t('payment.orders.exportHint') }}</p>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label for="order-export-start" class="input-label">{{ t('payment.orders.startDate') }}</label>
          <input id="order-export-start" v-model="exportStart" type="date" class="input mt-1 w-full" :max="exportEnd" :disabled="exporting" />
        </div>
        <div>
          <label for="order-export-end" class="input-label">{{ t('payment.orders.endDate') }}</label>
          <input id="order-export-end" v-model="exportEnd" type="date" class="input mt-1 w-full" :min="exportStart" :disabled="exporting" />
        </div>
      </div>
      <p class="mt-3 text-sm text-gray-600 dark:text-gray-300">{{ t('payment.orders.status') }}: {{ statusFilters.find(filter => filter.value === currentFilter)?.label }}</p>
      <p v-if="!exportRange" role="alert" class="mt-2 text-sm text-red-600 dark:text-red-400">{{ t('payment.orders.invalidRange') }}</p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" :disabled="exporting" @click="showExport = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" data-test="confirm-export" :disabled="exporting || !exportRange" @click="exportOrders">{{ exporting ? t('common.processing') : t('payment.orders.export') }}</button>
        </div>
      </template>
    </BaseDialog>

    <!-- Cancel Confirm Dialog -->
    <BaseDialog :show="!!cancelTarget" :title="t('payment.orders.cancel')" width="narrow" @close="cancelTarget = null">
      <p class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('payment.confirmCancel') }}
      </p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="cancelTarget = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-danger" :disabled="actionLoading" @click="confirmCancel">{{ actionLoading ? t('common.processing') : t('payment.orders.cancel') }}</button>
        </div>
      </template>
    </BaseDialog>

    <!-- Refund Dialog -->
    <BaseDialog :show="!!refundTarget" :title="t('payment.orders.requestRefund')" @close="refundTarget = null">
      <div v-if="refundTarget" class="space-y-4">
        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
          <div class="flex justify-between text-sm">
            <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.orderId') }}</span>
            <span class="font-mono text-gray-900 dark:text-white">#{{ refundTarget.id }}</span>
          </div>
          <div class="mt-2 flex justify-between text-sm">
            <span class="text-gray-500 dark:text-gray-400">{{ t('payment.orders.amount') }}</span>
            <span class="text-gray-900 dark:text-white">${{ refundTarget.amount.toFixed(2) }}</span>
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('payment.refundReason') }}</label>
          <textarea v-model="refundReason" rows="3" class="input mt-1 w-full" :placeholder="t('payment.refundReasonPlaceholder')" />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="refundTarget = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="actionLoading || !refundReason.trim()" @click="confirmRefund">{{ actionLoading ? t('common.processing') : t('payment.orders.requestRefund') }}</button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { getPaymentPopupFeatures, isolatePaymentPopup } from '@/components/payment/providerConfig'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  createPaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import type { PaymentOrder } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import OrderTable from '@/components/payment/OrderTable.vue'
import { orderExportRange, ordersCSV } from '@/components/payment/orderExport'
import { saveAs } from 'file-saver'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const loading = ref(false)
const actionLoading = ref(false)
const resumingOrderId = ref<number | null>(null)
const orders = ref<PaymentOrder[]>([])
const refundEligibleProviders = ref<Set<string>>(new Set())
const currentFilter = ref('')
const cancelTarget = ref<PaymentOrder | null>(null)
const refundTarget = ref<PaymentOrder | null>(null)
const refundReason = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })
const remarkTarget = ref<PaymentOrder | null>(null)
const remarkDraft = ref('')
const savingRemark = ref(false)
const showExport = ref(false)
const exporting = ref(false)
const today = new Date()
const localDate = (date: Date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
const exportStart = ref(localDate(new Date(today.getFullYear(), today.getMonth(), 1)))
const exportEnd = ref(localDate(today))
const exportRange = computed(() => orderExportRange(exportStart.value, exportEnd.value))

function openRemarkDialog(order: PaymentOrder) {
  remarkTarget.value = order
  remarkDraft.value = order.remark || ''
}

async function saveRemark() {
  if (!remarkTarget.value || savingRemark.value) return
  savingRemark.value = true
  try {
    const result = await paymentAPI.updateOrderRemark(remarkTarget.value.id, remarkDraft.value)
    remarkTarget.value.remark = result.data.remark
    remarkTarget.value = null
    appStore.showSuccess(t('common.success'))
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    savingRemark.value = false
  }
}

async function exportOrders() {
  if (!exportRange.value || exporting.value) return
  exporting.value = true
  try {
    const result = await paymentAPI.exportMyOrders({ ...exportRange.value, status: currentFilter.value || undefined })
    if (!result.data.length) {
      appStore.showError(t('payment.orders.noExportOrders'))
      return
    }
    saveAs(new Blob([ordersCSV(result.data, t)], { type: 'text/csv;charset=utf-8' }), `orders_${exportStart.value}_${exportEnd.value}.csv`)
    showExport.value = false
    appStore.showSuccess(t('common.success'))
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    exporting.value = false
  }
}

const statusFilters = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'PENDING', label: t('payment.status.pending') },
  { value: 'COMPLETED', label: t('payment.status.completed') },
  { value: 'FAILED', label: t('payment.status.failed') },
  { value: 'REFUNDED', label: t('payment.status.refunded') },
])

async function fetchOrders() {
  loading.value = true
  try {
    const res = await paymentAPI.getMyOrders({
      page: pagination.page,
      page_size: pagination.page_size,
      status: currentFilter.value || undefined,
    })
    orders.value = res.data.items || []
    pagination.total = res.data.total || 0
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    loading.value = false
  }
}

function handlePageChange(page: number) { pagination.page = page; fetchOrders() }
function handlePageSizeChange(size: number) { pagination.page_size = size; pagination.page = 1; fetchOrders() }

function handleCancel(order: PaymentOrder) { cancelTarget.value = order }

async function handleResume(order: PaymentOrder) {
  if (!order.resumable || order.status !== 'PENDING' || resumingOrderId.value) return
  const popup = isolatePaymentPopup(window.open('', 'paymentPopup', getPaymentPopupFeatures()))
  if (!popup || popup.closed) {
    appStore.showError(t('payment.qr.popupBlocked'))
    return
  }

  resumingOrderId.value = order.id
  try {
    const response = await paymentAPI.resumeOrder(order.id)
    const qrCode = String(response.data.qr_code || '').trim()
    if (qrCode) {
      if (!popup.closed) popup.close()
      const recovery = createPaymentRecoverySnapshot({
        orderId: response.data.order_id,
        amount: response.data.amount,
        qrCode,
        expiresAt: response.data.expires_at,
        paymentType: response.data.payment_type,
        payUrl: '',
        outTradeNo: response.data.out_trade_no,
        clientSecret: '',
        intentId: '',
        currency: response.data.currency,
        countryCode: '',
        paymentEnv: '',
        payAmount: response.data.pay_amount,
        orderType: response.data.order_type,
        paymentMode: 'qrcode',
        resumeToken: '',
        resumable: true,
        alipayMobilePrecreateDeepLink: false,
      })
      writePaymentRecoverySnapshot(window.localStorage, recovery, PAYMENT_RECOVERY_STORAGE_KEY)
      await router.push('/purchase')
      return
    }
    const payURL = String(response.data.pay_url || '').trim()
    if (!payURL) throw new Error(t('payment.qr.resumeFailed'))
    popup.location.href = payURL
  } catch (err: unknown) {
    if (!popup.closed) popup.close()
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('payment.qr.resumeFailed')))
  } finally {
    resumingOrderId.value = null
  }
}

async function confirmCancel() {
  if (!cancelTarget.value) return
  actionLoading.value = true
  try {
    await paymentAPI.cancelOrder(cancelTarget.value.id)
    appStore.showSuccess(t('common.success'))
    cancelTarget.value = null
    await fetchOrders()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    actionLoading.value = false
  }
}

function openRefundDialog(order: PaymentOrder) { refundTarget.value = order; refundReason.value = '' }

async function confirmRefund() {
  if (!refundTarget.value || !refundReason.value.trim()) return
  actionLoading.value = true
  try {
    await paymentAPI.requestRefund(refundTarget.value.id, { reason: refundReason.value.trim() })
    appStore.showSuccess(t('common.success'))
    refundTarget.value = null
    refundReason.value = ''
    await fetchOrders()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    actionLoading.value = false
  }
}

function canRequestRefund(order: PaymentOrder): boolean {
  if (order.status !== 'COMPLETED') return false
  if (!order.provider_instance_id) return false
  return refundEligibleProviders.value.has(order.provider_instance_id)
}

async function loadRefundEligibility() {
  try {
    const res = await paymentAPI.getRefundEligibleProviders()
    refundEligibleProviders.value = new Set(res.data.provider_instance_ids || [])
  } catch { /* ignore — default to hiding refund button */ }
}

onMounted(() => { fetchOrders(); loadRefundEligibility() })
</script>
