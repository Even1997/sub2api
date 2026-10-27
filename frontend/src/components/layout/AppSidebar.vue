<template>
  <aside
    id="app-sidebar"
    class="sidebar"
    @keydown.esc="closeMobile"
    :inert="!isDesktop && !mobileOpen"
    :aria-hidden="!isDesktop && !mobileOpen ? true : undefined"
    :class="[
      sidebarCollapsed ? 'w-[72px]' : 'w-64',
      { '-translate-x-full lg:translate-x-0': !mobileOpen }
    ]"
  >
    <!-- Logo/Brand -->
    <div class="sidebar-header" :class="{ 'sidebar-header-collapsed': sidebarCollapsed }">
      <!-- Custom Logo or Default Logo -->
      <router-link
        :to="homePath"
        class="sidebar-logo flex h-9 w-9 items-center justify-center overflow-hidden rounded-lg shadow-sm transition-opacity hover:opacity-80"
        @click="handleMenuItemClick(homePath)"
      >
        <img v-if="settingsLoaded" :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
      </router-link>
      <div class="sidebar-brand" :class="{ 'sidebar-brand-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
        <router-link
          :to="homePath"
          class="sidebar-brand-title text-lg font-bold text-gray-900 transition-colors hover:text-primary-600 dark:text-white dark:hover:text-primary-400"
          @click="handleMenuItemClick(homePath)"
        >
          {{ siteName }}
        </router-link>
        <!-- Version Badge -->
        <VersionBadge :version="siteVersion" />
      </div>
      <button v-if="!isDesktop" class="ml-auto rounded-md p-2" :aria-label="t('common.close')" @click="closeMobile"><Icon name="x" size="md" /></button>
    </div>

    <FeatureSearch v-if="isAdmin" :items="searchNavItems" :collapsed="sidebarCollapsed" @navigate="handleMenuItemClick" />

    <!-- Navigation -->
    <nav ref="sidebarNavRef" class="sidebar-nav scrollbar-hide">
      <div v-if="authStore.isObserver" class="sidebar-section">
        <router-link to="/admin/accounts" class="sidebar-link mb-1"
          :class="{ 'sidebar-link-active': isActive('/admin/accounts'), 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="sidebarCollapsed ? t('nav.accounts') : undefined"
          @click="handleMenuItemClick('/admin/accounts')">
          <GlobeIcon class="h-5 w-5 flex-shrink-0" />
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }">{{ t('nav.accounts') }}</span>
        </router-link>
        <router-link v-if="appStore.backendModeEnabled" to="/usage" class="sidebar-link mb-1"
          :class="{ 'sidebar-link-active': isActive('/usage'), 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="sidebarCollapsed ? t('nav.usage') : undefined"
          @click="handleMenuItemClick('/usage')">
          <ChartIcon class="h-5 w-5 flex-shrink-0" />
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }">{{ t('nav.usage') }}</span>
        </router-link>
      </div>
      <!-- Admin View: Admin menu first, then personal menu -->
      <template v-if="isAdmin">
        <!-- Admin Section -->
        <div class="sidebar-section">
          <template v-for="item in adminNavItems" :key="item.path">
            <!-- Collapsible group (has children) -->
            <template v-if="item.children?.length">
              <button
                type="button"
                class="sidebar-link mb-1 w-full"
                :class="{
                  'sidebar-link-active': isGroupActive(item) && !isGroupExpanded(item),
                  'sidebar-link-collapsed': sidebarCollapsed
                }"
                :title="sidebarCollapsed ? item.label : undefined"
                :aria-label="item.label"
                :aria-expanded="!sidebarCollapsed && isGroupExpanded(item)"
                @click="handleGroupClick(item)"
              >
                <component :is="item.icon" class="h-5 w-5 flex-shrink-0" />
                <span
                  class="sidebar-label sidebar-label-flex"
                  :class="{ 'sidebar-label-collapsed': sidebarCollapsed }"
                  :aria-hidden="sidebarCollapsed ? 'true' : 'false'"
                >
                  <span class="min-w-0 truncate">{{ item.label }}</span>
                  <ChevronDownIcon
                    class="h-4 w-4 flex-shrink-0 transition-transform duration-200"
                    :class="isGroupExpanded(item) ? 'rotate-180' : ''"
                  />
                </span>
              </button>
              <!-- Children -->
              <div v-if="!sidebarCollapsed && isGroupExpanded(item)" class="mb-1 ml-4 border-l border-gray-200 pl-2 dark:border-dark-600">
                <router-link
                  v-for="child in item.children"
                  :key="child.path"
                  :to="child.path"
                  class="sidebar-link mb-0.5 py-1.5 text-sm"
                  :class="{ 'sidebar-link-active': route.path === child.path }"
                  @click="handleMenuItemClick(child.path)"
                >
                  <component :is="child.icon" class="h-4 w-4 flex-shrink-0" />
                  <span>{{ child.label }}</span>
                </router-link>
              </div>
            </template>
            <!-- Normal item (no children) -->
            <router-link
              v-else
              :to="item.path"
              class="sidebar-link mb-1"
              :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
              :title="sidebarCollapsed ? item.label : undefined"
              :aria-label="item.label"
              :id="
                item.path === '/admin/accounts'
                  ? 'sidebar-channel-manage'
                  : item.path === '/admin/groups'
                    ? 'sidebar-group-manage'
                    : item.path === '/admin/redeem'
                      ? 'sidebar-wallet'
                      : undefined
              "
              @click="handleMenuItemClick(item.path)"
            >
              <span v-if="item.iconSvg" class="h-5 w-5 flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
              <component v-else :is="item.icon" class="h-5 w-5 flex-shrink-0" />
              <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
              <span v-if="navBadge(item)" class="sidebar-nav-badge" :class="{ 'sidebar-nav-badge-collapsed': sidebarCollapsed }" data-testid="sidebar-nav-badge">{{ sidebarCollapsed ? '' : navBadgeText(item) }}</span>
            </router-link>
          </template>
        </div>

        <!-- Personal Section for Admin (hidden in simple mode) -->
        <div v-if="!authStore.isSimpleMode" class="sidebar-section">
          <div class="sidebar-section-title" :class="{ 'sidebar-section-title-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
            <span class="sidebar-section-title-text" :class="{ 'sidebar-section-title-text-collapsed': sidebarCollapsed }">
              {{ t('nav.myAccount') }}
            </span>
          </div>

          <router-link
            v-for="item in personalNavItems"
            :key="item.path"
            :to="item.path"
            class="sidebar-link mb-1"
            :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
            :title="sidebarCollapsed ? item.label : undefined"
            :aria-label="item.label"
            :data-tour="item.path === '/keys' ? 'sidebar-my-keys' : undefined"
            @click="handleMenuItemClick(item.path)"
          >
            <span v-if="item.iconSvg" class="h-5 w-5 flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
            <component v-else :is="item.icon" class="h-5 w-5 flex-shrink-0" />
            <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
            <span v-if="navBadge(item)" class="sidebar-nav-badge" :class="{ 'sidebar-nav-badge-collapsed': sidebarCollapsed }" data-testid="sidebar-nav-badge">{{ sidebarCollapsed ? '' : navBadgeText(item) }}</span>
          </router-link>
        </div>
      </template>

      <!-- Regular User View -->
      <template v-else-if="!appStore.backendModeEnabled">
        <div class="sidebar-section">
          <div class="sidebar-section-title" :class="{ 'sidebar-section-title-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
            <span class="sidebar-section-title-text" :class="{ 'sidebar-section-title-text-collapsed': sidebarCollapsed }">{{ t('nav.myAccount') }}</span>
          </div>
          <router-link
            v-for="item in userNavItems"
            :key="item.path"
            :to="item.path"
            class="sidebar-link mb-1"
            :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
            :title="sidebarCollapsed ? item.label : undefined"
            :aria-label="item.label"
            :data-tour="item.path === '/keys' ? 'sidebar-my-keys' : undefined"
            @click="handleMenuItemClick(item.path)"
          >
            <span v-if="item.iconSvg" class="h-5 w-5 flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
            <component v-else :is="item.icon" class="h-5 w-5 flex-shrink-0" />
            <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
            <span v-if="navBadge(item)" class="sidebar-nav-badge" :class="{ 'sidebar-nav-badge-collapsed': sidebarCollapsed }" data-testid="sidebar-nav-badge">{{ sidebarCollapsed ? '' : navBadgeText(item) }}</span>
          </router-link>
        </div>
      </template>
    </nav>

    <!-- Bottom Section -->
    <div class="sidebar-footer mt-auto border-t border-gray-100 p-3 dark:border-dark-800">
      <!-- Theme Toggle -->
      <button
        @click="toggleTheme()"
        class="sidebar-link mb-2 w-full"
        :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
        :title="sidebarCollapsed ? (isDark ? t('nav.lightMode') : t('nav.darkMode')) : undefined"
        :aria-label="isDark ? t('nav.lightMode') : t('nav.darkMode')"
      >
        <SunIcon v-if="isDark" class="h-5 w-5 flex-shrink-0 text-amber-500" />
        <MoonIcon v-else class="h-5 w-5 flex-shrink-0" />
        <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{
          isDark ? t('nav.lightMode') : t('nav.darkMode')
        }}</span>
      </button>

      <!-- Collapse Button -->
      <button
        v-if="isDesktop"
        @click="toggleSidebar"
        class="sidebar-link w-full"
        :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
        :title="sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
        :aria-label="sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
      >
        <ChevronDoubleLeftIcon v-if="!sidebarCollapsed" class="h-5 w-5 flex-shrink-0" />
        <ChevronDoubleRightIcon v-else class="h-5 w-5 flex-shrink-0" />
        <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ t('nav.collapse') }}</span>
      </button>
    </div>
  </aside>

  <!-- Mobile Overlay -->
  <transition name="fade">
    <div
      v-if="mobileOpen"
      class="fixed inset-0 z-30 bg-black/50 lg:hidden"
      @click="closeMobile"
    ></div>
  </transition>
</template>

<script setup lang="ts">
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useDark, useToggle, useMediaQuery } from '@vueuse/core'
import { useAdminSettingsStore, useAppStore, useAuthStore, useOnboardingStore, useSupportTicketStore } from '@/stores'
import VersionBadge from '@/components/common/VersionBadge.vue'
import FeatureSearch from './FeatureSearch.vue'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeSvg } from '@/utils/sanitize'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, makeSidebarFlag } from '@/utils/featureFlags'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { getCustomMenuLabel } from '@/utils/customMenu'

const keyPackagesEnabled = import.meta.env.VITE_ENABLE_KEY_PACKAGES !== 'false'

interface NavItem {
  path: string
  label: string
  icon: unknown
  iconSvg?: string
  hideInSimpleMode?: boolean
  children?: NavItem[]
  /**
   * When true, the parent item only toggles the expand/collapse state and
   * does NOT navigate to its `path`. The `path` is purely a stable key.
   */
  expandOnly?: boolean
  /**
   * 可选的功能开关 getter。返回 false 时菜单项被隐藏；返回 undefined/true 时显示。
   * 宽容策略（undefined → 显示）避免 public settings 未加载完成时菜单闪烁消失。
   * Getter 里访问的 reactive 来源（store / composable）会被 computed 自动追踪，
   * 开关切换时菜单自动更新。
   */
  featureFlag?: () => boolean | undefined
  /** Optional count shown as a red badge (a dot while the sidebar is collapsed). */
  badge?: () => number
}

// applyFeatureFlags 递归过滤掉 featureFlag() === false 的节点（含子节点）。
// 使用 `!== false` 宽容语义：undefined（设置未加载）或 true 都视为显示。
function applyFeatureFlags(items: NavItem[]): NavItem[] {
  const out: NavItem[] = []
  for (const item of items) {
    if (item.featureFlag && item.featureFlag() === false) continue
    if (item.children) {
      out.push({ ...item, children: applyFeatureFlags(item.children) })
    } else {
      out.push(item)
    }
  }
  return out
}

const { t, locale } = useI18n()

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()
const adminSettingsStore = useAdminSettingsStore()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

const isDesktop = useMediaQuery('(min-width: 1024px)')
const sidebarCollapsed = computed(() => isDesktop.value && appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)
const isAdmin = computed(() => authStore.isAdmin)
const sidebarNavRef = ref<HTMLElement | null>(null)
const isDark = useDark({ storageKey: 'theme' })
const toggleTheme = useToggle(isDark)

const homePath = computed(() => (
  isAdmin.value ? '/admin/dashboard' : authStore.isObserver ? '/admin/accounts' : '/dashboard'
))

// Per-group expand/collapse overrides. A group with no entry follows the
// automatic behavior (expanded while the active route is one of its children);
// a chevron click records the user's choice, which wins over the automatic
// state so an active group can still be collapsed manually.
const groupExpandOverrides = ref<Map<string, boolean>>(new Map())

// Site settings from appStore (cached, no flicker)
const siteName = computed(() => appStore.siteName)
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteVersion = computed(() => appStore.siteVersion)
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

// Navigation and controls share the same icon family.
const RequestCaptureIcon = { render: () => h(Icon, { name: 'requestCapture' }) }
const OpsMonitoringIcon = { render: () => h(Icon, { name: 'monitorPulse' }) }
const SmartOpsIcon = { render: () => h(Icon, { name: 'cpu' }) }
const QualityOpsIcon = { render: () => h(Icon, { name: 'badge' }) }
const AccountOpsIcon = { render: () => h(Icon, { name: 'userCog' }) }
const TokenGuardIcon = { render: () => h(Icon, { name: 'shieldKey' }) }
const CredentialOpsIcon = { render: () => h(Icon, { name: 'credentialOps' }) }
const SupportTicketIcon = { render: () => h(Icon, { name: 'chat' }) }
const SupportTicketInboxIcon = { render: () => h(Icon, { name: 'inbox' }) }
const DashboardIcon = { render: () => h(Icon, { name: 'grid' }) }
const KeyIcon = { render: () => h(Icon, { name: 'key' }) }
const PackageIcon = { render: () => h(Icon, { name: 'cube' }) }
const DownloadIcon = { render: () => h(Icon, { name: 'download' }) }
const BatchImageIcon = { render: () => h(Icon, { name: 'camera' }) }
const ChartIcon = { render: () => h(Icon, { name: 'chart' }) }
const GiftIcon = { render: () => h(Icon, { name: 'gift' }) }
const UserIcon = { render: () => h(Icon, { name: 'user' }) }
const UsersIcon = { render: () => h(Icon, { name: 'users' }) }
const FolderIcon = { render: () => h(Icon, { name: 'folder' }) }
const ChannelIcon = { render: () => h(Icon, { name: 'layers' }) }
const CreditCardIcon = { render: () => h(Icon, { name: 'creditCard' }) }
const RechargeSubscriptionIcon = { render: () => h(Icon, { name: 'wallet' }) }
const GlobeIcon = { render: () => h(Icon, { name: 'globe' }) }
const ServerIcon = { render: () => h(Icon, { name: 'server' }) }
const PluginIcon = { render: () => h(Icon, { name: 'cube' }) }
const BellIcon = { render: () => h(Icon, { name: 'bell' }) }
const TicketIcon = { render: () => h(Icon, { name: 'receipt' }) }
const CogIcon = { render: () => h(Icon, { name: 'cog' }) }
const SunIcon = { render: () => h(Icon, { name: 'sun' }) }
const MoonIcon = { render: () => h(Icon, { name: 'moon' }) }
const ChevronDoubleLeftIcon = { render: () => h(Icon, { name: 'chevronsLeft' }) }
const OrderIcon = { render: () => h(Icon, { name: 'receipt' }) }
const OrderListIcon = { render: () => h(Icon, { name: 'list' }) }
const BillIcon = { render: () => h(Icon, { name: 'document' }) }
const ChevronDoubleRightIcon = { render: () => h(Icon, { name: 'chevronsRight' }) }
const SignalIcon = { render: () => h(Icon, { name: 'radio' }) }
const FlowIcon = { render: () => h(Icon, { name: 'route' }) }
const GalleryIcon = { render: () => h(Icon, { name: 'image' }) }
const ShieldIcon = { render: () => h(Icon, { name: 'shield' }) }
const PriceTagIcon = { render: () => h(Icon, { name: 'tag' }) }
const ChevronDownIcon = { render: () => h(Icon, { name: 'chevronDown' }) }

// Public-settings flags go through the registry in utils/featureFlags.ts,
// which handles the opt-in vs opt-out fallback when settings haven't loaded
// yet. Admin-only flags (not in public settings) stay inline below.
const flagChannelMonitor = makeSidebarFlag(FeatureFlags.channelMonitor)
const flagPayment = makeSidebarFlag(FeatureFlags.payment)
const flagAvailableChannels = makeSidebarFlag(FeatureFlags.availableChannels)
const flagPelicanShowcase = makeSidebarFlag(FeatureFlags.pelicanShowcase)
const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)
const flagDesktop = makeSidebarFlag(FeatureFlags.desktop)

// 购买入口文案随站点计费模式切换：仅充值 → 「充值」，仅订阅 → 「订阅」，否则「充值/订阅」。
const purchaseNavLabel = computed(() => {
  switch (resolveSiteBillingMode(appStore.cachedPublicSettings)) {
    case 'recharge_only':
      return t('nav.recharge')
    case 'subscription_only':
      return t('nav.subscribe')
    default:
      return t('nav.buySubscription')
  }
})
const flagAffiliate = makeSidebarFlag(FeatureFlags.affiliate)
const flagRiskControl = makeSidebarFlag(FeatureFlags.riskControl)
const flagPluginManagement = makeSidebarFlag(FeatureFlags.pluginManagement)
const flagSupportTickets = makeSidebarFlag(FeatureFlags.supportTickets)
// Admins answer tickets from their own menu, so their "my account" list skips it.
const flagUserSupportTickets = () => flagSupportTickets() && !authStore.isAdmin
const supportTicketStore = useSupportTicketStore()
const flagOpsMonitoring = () => adminSettingsStore.opsMonitoringEnabled
const flagAdminPayment = () => adminSettingsStore.paymentEnabled
const flagBatchImageAccess = () => canUseBatchImage.value

// buildSelfNavItems 构造用户自己的导航项（用户端主菜单和管理员的"我的账户"子菜单共享这组声明）。
// withDashboard=true 时包含仪表盘（用户端），false 时不含（管理员的个人区已经有独立仪表盘入口）。
//
// 条目顺序：密钥 → 用量 → 可用渠道 → 渠道状态 → 鹈鹕测智 → 订阅/支付 → 兑换/资料。
// 可用渠道紧挨渠道状态之上，让用户"先看自己能用什么、再看对应状态"。
function buildSelfNavItems(withDashboard: boolean): NavItem[] {
  const items: NavItem[] = []
  if (withDashboard) {
    items.push({ path: '/dashboard', label: t('nav.dashboard'), icon: DashboardIcon })
  }
  items.push({ path: '/keys', label: t('nav.apiKeys'), icon: KeyIcon })
  if (keyPackagesEnabled) {
    items.push({ path: '/packages', label: t('nav.keyPackages'), icon: PackageIcon })
  }
  items.push(
    { path: '/desktop', label: t('nav.desktop'), icon: DownloadIcon, featureFlag: flagDesktop },
    { path: '/batch-image', label: t('nav.batchImage'), icon: BatchImageIcon, hideInSimpleMode: true, featureFlag: flagBatchImageAccess },
    { path: '/usage', label: t('nav.usage'), icon: ChartIcon, hideInSimpleMode: !authStore.isObserver },
    { path: '/available-channels', label: t('nav.availableChannels'), icon: ChannelIcon, hideInSimpleMode: true, featureFlag: flagAvailableChannels },
    { path: '/monitor', label: t('nav.channelStatus'), icon: SignalIcon, featureFlag: flagChannelMonitor },
    { path: '/pelican-showcase', label: t('nav.pelicanShowcase'), icon: GalleryIcon, featureFlag: flagPelicanShowcase },
    { path: '/subscriptions', label: t('nav.mySubscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flagSubscription },
    { path: '/purchase', label: purchaseNavLabel.value, icon: RechargeSubscriptionIcon, hideInSimpleMode: true, featureFlag: flagPayment },
    { path: '/bills', label: t('bills.title'), icon: BillIcon, hideInSimpleMode: true },
    { path: '/orders', label: t('nav.myOrders'), icon: OrderListIcon, hideInSimpleMode: true, featureFlag: flagPayment },
    { path: '/redeem', label: t('nav.redeem'), icon: GiftIcon, hideInSimpleMode: true },
    { path: '/affiliate', label: t('nav.affiliate'), icon: UsersIcon, hideInSimpleMode: true, featureFlag: flagAffiliate },
    { path: '/support-tickets', label: t('nav.supportTickets'), icon: SupportTicketIcon, featureFlag: flagUserSupportTickets, badge: () => supportTicketStore.userUnread },
    { path: '/security-records', label: t('securityRecords.title'), icon: ShieldIcon },
    { path: '/profile', label: t('nav.profile'), icon: UserIcon },
    ...customMenuItemsForUser.value.map((item): NavItem => ({
      path: `/custom/${item.id}`,
      label: getCustomMenuLabel(item, locale.value),
      icon: null,
      iconSvg: item.icon_svg,
    })),
  )
  return items
}

// finalizeNav 合并三重过滤：featureFlag 过滤 + simple 模式过滤。
function finalizeNav(items: NavItem[]): NavItem[] {
  const visible = applyFeatureFlags(items)
  return authStore.isSimpleMode ? visible.filter(item => !item.hideInSimpleMode) : visible
}

// User navigation items (for regular users)
const userNavItems = computed((): NavItem[] => finalizeNav(buildSelfNavItems(true)))

// Personal navigation items (for admin's "My Account" section, without Dashboard).
// Admins access 可用渠道 from this section just like regular users — there is no
// separate admin entry, since the page is purely a user-facing view.
const personalNavItems = computed((): NavItem[] => finalizeNav(buildSelfNavItems(false)))

// Custom menu items filtered by visibility
const customMenuItemsForUser = computed(() => {
  const items = appStore.cachedPublicSettings?.custom_menu_items ?? []
  return items
    .filter((item) => item.visibility === 'user')
    .sort((a, b) => a.sort_order - b.sort_order)
})

const customMenuItemsForAdmin = computed(() => {
  return adminSettingsStore.customMenuItems
    .filter((item) => item.visibility === 'admin')
    .sort((a, b) => a.sort_order - b.sort_order)
})

// Admin navigation items
const adminNavItems = computed((): NavItem[] => {
  const baseItems: NavItem[] = [
    { path: '/admin/dashboard', label: t('nav.dashboard'), icon: DashboardIcon },
    { path: '/admin/request-captures', label: t('admin.requestCapture.title'), icon: RequestCaptureIcon, featureFlag: () => adminSettingsStore.requestCaptureEnabled },
    { path: '/admin/ops', label: t('nav.ops'), icon: OpsMonitoringIcon, featureFlag: flagOpsMonitoring },
    { path: '/admin/users', label: t('nav.users'), icon: UsersIcon, hideInSimpleMode: true },
    { path: '/admin/groups', label: t('nav.groups'), icon: FolderIcon },
    {
      path: '/admin/channels',
      label: t('nav.channelManagement'),
      icon: ChannelIcon,
      hideInSimpleMode: true,
      expandOnly: true,
      children: [
        { path: '/admin/channels/pricing', label: t('nav.channelPricing'), icon: PriceTagIcon },
        { path: '/admin/channels/monitor', label: t('nav.channelMonitor'), icon: SignalIcon, featureFlag: flagChannelMonitor },
      ],
    },
    // 「仅充值」站点连管理端的「订阅管理」入口也一并收起（路由本身不拦截）。
    { path: '/admin/subscriptions', label: t('nav.subscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flagSubscription },
    { path: '/admin/accounts', label: t('nav.accounts'), icon: GlobeIcon },
    { path: '/admin/smart-ops', label: t('accountOps.smartTitle'), icon: SmartOpsIcon, expandOnly: true, children: [
      { path: '/admin/auto-config', label: t('autoConfig.title'), icon: AccountOpsIcon },
      { path: '/admin/priority-scheduling', label: t('priorityScheduling.title'), icon: AccountOpsIcon },
      { path: '/admin/channel-operations', label: t('channelOps.title'), icon: ChannelIcon },
      { path: '/admin/account-quality', label: t('qualityOps.title'), icon: QualityOpsIcon },
      { path: '/admin/controlled-experiments', label: t('controlledExperiments.title'), icon: QualityOpsIcon },
      { path: '/admin/account-ops', label: t('accountOps.title'), icon: AccountOpsIcon },
      { path: '/admin/token-guard', label: t('tokenGuard.title'), icon: TokenGuardIcon },
      { path: '/admin/token-guard-v2', label: t('tokenGuardV2.title'), icon: CredentialOpsIcon },
      { path: '/admin/pelican-tests', label: t('pelicanTests.title'), icon: QualityOpsIcon },
      { path: '/admin/group-cache-tests', label: t('admin.groupCacheTest.title'), icon: QualityOpsIcon },
      { path: '/admin/gateway-borrow', label: t('nav.gatewayBorrow'), icon: FlowIcon },
    ] },
    { path: '/admin/harvest-flow', label: t('nav.harvestFlow'), icon: FlowIcon },
    { path: '/admin/plugins', label: t('nav.plugins'), icon: PluginIcon, featureFlag: flagPluginManagement },
    { path: '/admin/announcements', label: t('nav.announcements'), icon: BellIcon },
    { path: '/admin/support-tickets', label: t('nav.supportTickets'), icon: SupportTicketInboxIcon, featureFlag: flagSupportTickets, badge: () => supportTicketStore.adminPending },
    { path: '/admin/proxies', label: t('nav.proxies'), icon: ServerIcon },
    {
      path: '/admin/security-audit',
      label: t('nav.securityAudit'),
      icon: ShieldIcon,
      expandOnly: true,
      children: [
        { path: '/admin/risk-control', label: t('nav.contentModeration'), icon: ShieldIcon, featureFlag: flagRiskControl },
        { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: ShieldIcon, featureFlag: flagRiskControl },
        { path: '/admin/safety-prompt-audit', label: t('admin.safetyPromptAudit.nav'), icon: ShieldIcon, featureFlag: flagRiskControl },
        { path: '/admin/ip-audit', label: t('ipAudit.title'), icon: ShieldIcon },
      ],
    },
    { path: '/admin/redeem', label: t('nav.redeemCodes'), icon: TicketIcon, hideInSimpleMode: true },
    { path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: GiftIcon, hideInSimpleMode: true },
    {
      path: '/admin/affiliates',
      label: t('nav.affiliateManagement'),
      icon: UsersIcon,
      hideInSimpleMode: true,
      expandOnly: true,
      featureFlag: flagAffiliate,
      children: [
        { path: '/admin/affiliates/invites', label: t('nav.affiliateInviteRecords'), icon: UsersIcon },
        { path: '/admin/affiliates/rebates', label: t('nav.affiliateRebateRecords'), icon: OrderIcon },
        { path: '/admin/affiliates/transfers', label: t('nav.affiliateTransferRecords'), icon: CreditCardIcon },
      ],
    },
    {
      path: '/admin/orders',
      label: t('nav.orderManagement'),
      icon: OrderIcon,
      hideInSimpleMode: true,
      expandOnly: true,
      featureFlag: flagAdminPayment,
      children: [
        { path: '/admin/orders/dashboard', label: t('nav.paymentDashboard'), icon: ChartIcon },
        { path: '/admin/orders', label: t('nav.orderManagement'), icon: OrderIcon },
        { path: '/admin/orders/plans', label: t('nav.paymentPlans'), icon: CreditCardIcon },
      ],
    },
    { path: '/admin/usage', label: t('nav.usage'), icon: ChartIcon },
    { path: '/admin/audit-logs', label: t('nav.auditLogs'), icon: ShieldIcon, hideInSimpleMode: true }
  ]

  const visible = applyFeatureFlags(baseItems)

  // 简单模式下，在系统设置前插入 API密钥
  if (authStore.isSimpleMode) {
    const filtered = visible.filter(item => !item.hideInSimpleMode)
    filtered.push({ path: '/keys', label: t('nav.apiKeys'), icon: KeyIcon })
    if (keyPackagesEnabled) {
      filtered.push({ path: '/packages', label: t('nav.keyPackages'), icon: PackageIcon })
    }
    if (flagDesktop()) {
      filtered.push({ path: '/desktop', label: t('nav.desktop'), icon: DownloadIcon, featureFlag: flagDesktop })
    }
    filtered.push({ path: '/admin/settings', label: t('nav.settings'), icon: CogIcon })
    for (const cm of customMenuItemsForAdmin.value) {
      filtered.push({
        path: `/custom/${cm.id}`,
        label: getCustomMenuLabel(cm, locale.value),
        icon: null,
        iconSvg: cm.icon_svg,
      })
    }
    return filtered
  }

  visible.push({ path: '/admin/settings', label: t('nav.settings'), icon: CogIcon })
  for (const cm of customMenuItemsForAdmin.value) {
    visible.push({
      path: `/custom/${cm.id}`,
      label: getCustomMenuLabel(cm, locale.value),
      icon: null,
      iconSvg: cm.icon_svg,
    })
  }
  return visible
})

// Use exactly the visible navigation, including the personal section only when shown.
const searchNavItems = computed(() => [
  ...adminNavItems.value,
  ...(authStore.isSimpleMode ? [] : personalNavItems.value)
])

function toggleSidebar() {
  appStore.toggleSidebar()
}

function closeMobile() {
  appStore.setMobileOpen(false)
  document.getElementById('mobile-navigation-toggle')?.focus()
}

function handleMenuItemClick(itemPath: string) {
  if (mobileOpen.value) {
    appStore.setMobileOpen(false)
  }

  // Map paths to tour selectors
  const pathToSelector: Record<string, string> = {
    '/admin/groups': '#sidebar-group-manage',
    '/admin/accounts': '#sidebar-channel-manage',
    '/keys': '[data-tour="sidebar-my-keys"]'
  }

  const selector = pathToSelector[itemPath]
  if (selector && onboardingStore.isCurrentStep(selector)) {
    onboardingStore.nextStep(500)
  }
}

function navBadge(item: NavItem): number {
  return item.badge?.() ?? 0
}

function navBadgeText(item: NavItem): string {
  const count = navBadge(item)
  return count > 99 ? '99+' : String(count)
}

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(path + '/')
}

function isGroupActive(item: NavItem): boolean {
  if (!item.children) return false
  return item.children.some(child => route.path === child.path)
}

function isGroupExpanded(item: NavItem): boolean {
  const override = groupExpandOverrides.value.get(item.path)
  if (override !== undefined) return override
  return isGroupActive(item)
}

function toggleGroup(item: NavItem) {
  groupExpandOverrides.value.set(item.path, !isGroupExpanded(item))
}

/**
 * Click handler for collapsible parent items.
 * - When sidebar is collapsed: do nothing (children are not visible).
 * - When `expandOnly` is true: only toggle expand state.
 * - Otherwise (default, e.g. /admin/orders): navigate to the parent path
 *   (router-link semantics) and ensure the group is expanded.
 */
function handleGroupClick(item: NavItem) {
  if (sidebarCollapsed.value) return
  if (item.expandOnly) {
    toggleGroup(item)
    return
  }
  // Push to path and ensure expanded
  if (route.path !== item.path) {
    router.push(item.path)
  }
  groupExpandOverrides.value.set(item.path, true)
}

// Fetch admin settings (for feature-gated nav items like Ops).
watch(
  isAdmin,
  (v) => {
    if (v) {
      adminSettingsStore.fetch()
    }
  },
  { immediate: true }
)

onMounted(() => {
  void refreshBatchImageAccess()
  if (isAdmin.value) {
    adminSettingsStore.fetch()
  }
  // Restore sidebar scroll position after route change re-mounts the component
  if (appStore.sidebarScrollTop > 0 && sidebarNavRef.value) {
    void nextTick(() => {
      if (sidebarNavRef.value) {
        sidebarNavRef.value.scrollTop = appStore.sidebarScrollTop
      }
    })
  }
})

onBeforeUnmount(() => {
  if (sidebarNavRef.value) {
    appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop
  }
})
</script>

<style scoped>
.sidebar-logo {
  flex: 0 0 2.25rem;
  min-width: 2.25rem;
}

.sidebar-header-collapsed {
  gap: 0;
  padding-left: 1.125rem;
  padding-right: 1.125rem;
}

.sidebar-brand {
  min-width: 0;
  flex: 1 1 auto;
  white-space: nowrap;
  transition:
    max-width 0.22s ease,
    opacity 0.14s ease,
    transform 0.14s ease;
  max-width: 12rem;
}

.sidebar-brand-collapsed {
  max-width: 0;
  overflow: hidden;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}

.sidebar-brand-title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar-link-collapsed {
  gap: 0;
  padding-left: 0.875rem;
  padding-right: 0.875rem;
}

.sidebar-section-title {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 1.25rem;
  overflow: hidden;
  white-space: nowrap;
}

.sidebar-section-title-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    opacity 0.16s ease,
    transform 0.16s ease;
}

.sidebar-section-title::after {
  content: '';
  position: absolute;
  left: 0.75rem;
  right: 0.75rem;
  top: 50%;
  height: 1px;
  background: rgb(229 231 235);
  opacity: 0;
  transform: translateY(-50%);
  transition: opacity 0.18s ease;
}

.dark .sidebar-section-title::after {
  background: rgb(55 65 81);
}

.sidebar-section-title-text-collapsed {
  opacity: 0;
  transform: translateX(-4px);
}

.sidebar-section-title-collapsed::after {
  opacity: 1;
  transition-delay: 0.08s;
}

.sidebar-label {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    max-width 0.2s ease,
    opacity 0.12s ease,
    transform 0.12s ease;
  max-width: 12rem;
}

.sidebar-label-flex {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.sidebar-link {
  position: relative;
}

.sidebar-nav-badge {
  margin-left: auto;
  flex-shrink: 0;
  min-width: 1.25rem;
  height: 1.25rem;
  padding: 0 0.375rem;
  border-radius: 9999px;
  background: rgb(239 68 68);
  color: white;
  font-size: 0.6875rem;
  font-weight: 600;
  line-height: 1.25rem;
  text-align: center;
}

.sidebar-nav-badge-collapsed {
  position: absolute;
  top: 0.4rem;
  left: 1.85rem;
  min-width: 0;
  width: 0.5rem;
  height: 0.5rem;
  padding: 0;
}

.sidebar-label-collapsed {
  max-width: 0;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}

/* Custom SVG icon in sidebar: constrain size without overriding uploaded SVG colors */
.sidebar-svg-icon {
  color: currentColor;
}

.sidebar-svg-icon :deep(svg) {
  display: block;
  width: 1.25rem;
  height: 1.25rem;
}
</style>
