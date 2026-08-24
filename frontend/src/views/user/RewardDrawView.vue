<template>
  <AppLayout class="reward-draw-layout">
    <div class="reward-page">
      <section v-if="initialLoading" class="state-panel" aria-live="polite">
        <span class="loading-ring" aria-hidden="true"></span>
        <strong>正在清点羊铃</strong>
      </section>

      <section v-else-if="loadError || !overview" class="state-panel error-state" role="alert">
        <Icon name="exclamationCircle" size="lg" />
        <strong>召唤机暂时没有响应</strong>
        <button type="button" class="secondary-button" @click="loadData">
          <Icon name="refresh" size="sm" />
          重新连接
        </button>
      </section>

      <template v-else>
        <section class="arcade-hero" :aria-busy="drawing">
          <div class="intro-column">
            <div class="brand-lockup">小羊咩咩</div>
            <h1>咩咩<br>召唤机</h1>
            <p class="draw-rule">每消费 <strong>$1</strong> 获得 1 次召唤</p>
            <div class="bell-progress">
              <div><span>下一枚羊铃</span><strong>{{ progressPercent }}%</strong></div>
              <span class="progress-track"><i :style="{ width: `${progressPercent}%` }"></i></span>
            </div>
          </div>

          <div class="machine-column" :class="{ 'is-revealing': revealing }">
            <div class="machine-visual">
              <img
                src="/reward-draw/generated/sheep-gashapon-machine.webp"
                alt="装满羊羊胶囊的羊形召唤机"
                class="machine-art"
              >
              <div class="machine-readout" aria-live="polite">
                <template v-if="featuredComponent && !revealing">
                  <span>{{ poolLabel(featuredComponent.pool_id) }}</span>
                  <strong>{{ featuredComponent.name }}</strong>
                  <em>+{{ featuredComponent.wool_value }} 羊毛</em>
                </template>
                <template v-else>
                  <span>{{ revealing ? '羊铃响起' : '召唤机就绪' }}</span>
                  <strong>{{ revealing ? `${activePoolName}正在出卡` : '选择卡池开始召唤' }}</strong>
                  <em>{{ revealing ? '•••' : `${overview.wallet.ticket_balance} 次可用` }}</em>
                </template>
              </div>
            </div>
            <button v-if="revealing && pendingBatch" type="button" class="skip-button" @click="finishReveal">
              跳过动画
            </button>
          </div>

          <div class="control-console">
            <div class="console-heading">
              <div>
                <small>当前卡池</small>
                <h2>选择{{ activePoolName }}</h2>
              </div>
              <div class="ticket-balance">
                <Icon name="bell" size="sm" />
                <span>{{ overview.wallet.ticket_balance }}</span>
                <small>羊铃</small>
              </div>
            </div>

            <div class="pool-selector" role="tablist" aria-label="召唤卡池">
              <button
                v-for="pool in overview.pools"
                :key="pool.id"
                type="button"
                role="tab"
                :data-testid="`pool-${pool.id}`"
                :aria-selected="activePool === pool.id"
                :class="{ active: activePool === pool.id }"
                @click="activePool = pool.id"
              >
                <Icon :name="poolIcon(pool.id)" size="lg" />
                <strong>{{ pool.name }}</strong>
                <small>{{ poolHint(pool.id) }}</small>
                <span v-if="activePool === pool.id" class="selected-mark" aria-hidden="true">
                  <Icon name="check" size="xs" />
                </span>
              </button>
            </div>

            <div class="draw-actions" role="group" aria-label="召唤次数">
              <button
                type="button"
                class="draw-button draw-once"
                data-testid="draw-1"
                :disabled="!canDraw(1)"
                @click="startDraw(1)"
              >
                <Icon name="play" size="md" />
                <span>召唤 1 次</span>
                <strong class="draw-cost" aria-label="消耗 1 枚羊铃">
                  <Icon name="bell" size="sm" aria-hidden="true" />
                  1
                </strong>
              </button>
              <button
                type="button"
                class="draw-button draw-ten"
                data-testid="draw-10"
                :disabled="!canDraw(10)"
                @click="startDraw(10)"
              >
                <Icon name="sparkles" size="md" />
                <span>召唤 10 次</span>
                <strong class="draw-cost" aria-label="消耗 10 枚羊铃">
                  <Icon name="bell" size="sm" aria-hidden="true" />
                  10
                </strong>
              </button>
              <button
                type="button"
                class="draw-button draw-all"
                data-testid="draw-all"
                :disabled="drawAllCount < 1 || drawing"
                @click="startDraw(drawAllCount)"
              >
                <Icon name="gift" size="md" />
                <span>全部召唤</span>
                <strong>{{ drawAllCount }} 次</strong>
              </button>
            </div>

            <p class="sync-note">
              <Icon name="sync" size="sm" />
              本次只会获得{{ activePoolName }}组件卡
            </p>

            <nav class="console-tools" aria-label="召唤机工具">
              <button type="button" data-testid="open-inventory-dialog" aria-haspopup="dialog" @click="openInventory(activePool)">
                <Icon name="cube" size="md" /><span>卡片图存</span>
              </button>
              <button type="button" data-testid="open-collection-dialog" aria-haspopup="dialog" @click="openDialog('collection')">
                <Icon name="book" size="md" /><span>羊羊图鉴</span>
              </button>
              <button type="button" data-testid="open-history-dialog" aria-haspopup="dialog" @click="openDialog('history')">
                <Icon name="clock" size="md" /><span>召唤历史</span>
              </button>
              <button type="button" data-testid="open-redeem-dialog" aria-haspopup="dialog" @click="openDialog('redeem')">
                <Icon name="dollar" size="md" /><span>兑换额度</span>
              </button>
            </nav>
          </div>
        </section>

        <p v-if="actionError" class="inline-error" role="alert">
          <Icon name="exclamationCircle" size="sm" />
          {{ actionError }}
        </p>

        <section class="recent-collection" aria-labelledby="recent-title">
          <div class="section-heading">
            <div><span class="status-dot" aria-hidden="true"></span><h2 id="recent-title">最近获得</h2></div>
            <button type="button" aria-haspopup="dialog" @click="openInventory(activePool)">
              查看图存 <Icon name="chevronRight" size="sm" />
            </button>
          </div>
          <div v-if="recentComponents.length" class="showcase-track">
            <button
              v-for="component in recentComponents"
              :key="component.id"
              type="button"
              class="showcase-card"
              :class="`tier-${component.tier_id}`"
              @click="openInventory(component.pool_id)"
            >
              <span class="rarity-tag">{{ component.rarity }}</span>
              <img :src="componentAsset(component)" :alt="component.name">
              <strong>{{ component.name }}</strong>
              <small>{{ poolLabel(component.pool_id) }} · {{ component.wool_value }} 羊毛</small>
            </button>
          </div>
          <div v-else class="recent-empty">第一张组件卡会出现在这里</div>
        </section>

        <BaseDialog
          :show="revealBatch !== null || activeDialog !== null"
          :title="dialogTitle"
          :width="dialogWidth"
          :close-on-click-outside="true"
          @close="closeDialog"
        >
          <section
            v-if="revealBatch"
            class="dialog-panel draw-result-panel"
            data-testid="draw-result-dialog"
          >
            <header class="reveal-summary">
              <span class="reveal-complete-icon" aria-hidden="true"><Icon name="sparkles" size="lg" /></span>
              <div>
                <small>{{ poolLabel(normalizePool(revealBatch.pool_id)) }}</small>
                <strong>召唤完成，{{ revealBatch.components.length }} 张组件已入库</strong>
                <span>本次结果已结算并保存至卡片图存</span>
              </div>
              <div class="reveal-wool">
                <small>本次羊毛</small>
                <strong>+{{ revealBatch.total_wool }}</strong>
              </div>
            </header>

            <div
              v-if="revealBatch.components.length"
              class="result-grid"
              :class="{ single: revealBatch.components.length === 1 }"
            >
              <article
                v-for="(component, index) in revealBatch.components"
                :key="component.id"
                class="result-card"
                :class="[`tier-${component.tier_id}`, { miracle: component.is_jackpot }]"
                :style="{ animationDelay: revealDelay(index) }"
              >
                <span class="result-rarity">{{ component.rarity }}</span>
                <div class="result-card-art"><img :src="componentAsset(component)" :alt="component.name"></div>
                <strong>{{ component.name }}</strong>
                <small>+{{ component.wool_value }} 羊毛</small>
              </article>
            </div>
            <div v-else class="empty-panel compact-empty">
              <Icon name="check" size="xl" />
              <strong>召唤结果已经入库</strong>
            </div>

            <footer class="reveal-actions">
              <button type="button" class="secondary-button" data-testid="reveal-continue" @click="closeDialog">
                继续召唤
              </button>
              <button type="button" class="reveal-inventory-button" data-testid="reveal-inventory" @click="openRevealInventory">
                <Icon name="cube" size="sm" />
                查看库存
              </button>
            </footer>
          </section>

          <div v-else-if="activeDialog === 'inventory'" class="dialog-panel inventory-panel">
            <div class="panel-toolbar">
              <p>{{ components.total }} 张组件卡 · {{ cards.total }} 张组合卡</p>
              <button type="button" class="secondary-button compact" @click="openDialog('redeem')">
                <Icon name="dollar" size="sm" /> 兑换卡牌
              </button>
            </div>

            <div class="inventory-kind-tabs" role="tablist" aria-label="卡片图存类型">
              <button
                type="button"
                role="tab"
                data-testid="inventory-components-tab"
                :aria-selected="inventoryMode === 'components'"
                :class="{ active: inventoryMode === 'components' }"
                @click="inventoryMode = 'components'"
              >
                <Icon name="cube" size="sm" />组件卡<span>{{ components.total }}</span>
              </button>
              <button
                type="button"
                role="tab"
                data-testid="inventory-cards-tab"
                :aria-selected="inventoryMode === 'cards'"
                :class="{ active: inventoryMode === 'cards' }"
                @click="inventoryMode = 'cards'"
              >
                <Icon name="sparkles" size="sm" />组合卡<span>{{ cards.total }}</span>
              </button>
            </div>

            <section v-if="inventoryMode === 'components'" class="compose-board" aria-labelledby="compose-title">
              <div class="compose-heading">
                <div><small>组合台</small><h3 id="compose-title">三张组件合成羊卡</h3></div>
                <strong>{{ combinePreviewWool }} 羊毛</strong>
              </div>
              <p class="recipe-hint" aria-live="polite">
                <Icon name="sparkles" size="sm" />
                <span>{{ currentRecipeHint }}</span>
              </p>
              <div class="compose-slots">
                <button
                  v-for="pool in poolIDs"
                  :key="pool"
                  type="button"
                  class="compose-slot"
                  :class="{ filled: selectedComponent(pool) }"
                  :aria-label="selectedComponent(pool) ? `移除 ${selectedComponent(pool)?.name}` : `选择${poolLabel(pool)}`"
                  @click="clearSelectedComponent(pool)"
                >
                  <template v-if="selectedComponent(pool)">
                    <img :src="componentAsset(selectedComponent(pool)!)" alt="">
                    <span>{{ selectedComponent(pool)?.name }}</span>
                    <small>{{ selectedComponent(pool)?.wool_value }} 羊毛</small>
                  </template>
                  <template v-else>
                    <Icon :name="poolIcon(pool)" size="lg" />
                    <span>{{ poolLabel(pool) }}</span>
                    <small>未选择</small>
                  </template>
                </button>
              </div>
              <div v-if="combineMaximum > 0" class="combine-quantity-row">
                <label for="combine-quantity">
                  <strong>合成数量</strong>
                  <small id="combine-quantity-limit">三组最少库存 {{ combineMaximum }} 张</small>
                </label>
                <div class="quantity-stepper">
                  <button
                    type="button"
                    aria-label="减少合成数量"
                    :disabled="combineQuantity <= 1 || combining"
                    @click="setCombineQuantity(combineQuantity - 1)"
                  >&minus;</button>
                  <input
                    id="combine-quantity"
                    data-testid="combine-quantity"
                    type="number"
                    inputmode="numeric"
                    min="1"
                    :max="combineMaximum"
                    :value="combineQuantity"
                    :disabled="combining"
                    aria-describedby="combine-quantity-limit"
                    @input="updateCombineQuantity"
                  >
                  <button
                    type="button"
                    aria-label="增加合成数量"
                    :disabled="combineQuantity >= combineMaximum || combining"
                    @click="setCombineQuantity(combineQuantity + 1)"
                  ><Icon name="plus" size="sm" /></button>
                  <button
                    type="button"
                    class="quantity-maximum"
                    :disabled="combineQuantity === combineMaximum || combining"
                    @click="setCombineQuantity(combineMaximum)"
                  >最大</button>
                </div>
              </div>
              <button
                type="button"
                class="combine-button"
                data-testid="combine-components"
                :disabled="!canCombine || combining"
                @click="combineSelected"
              >
                <Icon name="sparkles" size="sm" />
                {{ combineButtonLabel }}
              </button>
              <details class="recipe-guide">
                <summary>固定配方 · {{ overview.recipes.length }} 种</summary>
                <div class="recipe-list">
                  <div v-for="recipe in overview.recipes" :key="recipe.id" class="recipe-row" :class="`tier-${recipe.id}`">
                    <img :src="recipeAsset(recipe.id)" :alt="recipe.name">
                    <span>{{ recipeFormula(recipe) }}</span>
                    <strong>{{ recipe.wool }} 羊毛</strong>
                  </div>
                </div>
              </details>
            </section>

            <div v-if="inventoryMode === 'components'" class="pool-tabs" role="tablist" aria-label="组件卡池">
              <button
                v-for="pool in poolIDs"
                :key="pool"
                type="button"
                role="tab"
                :aria-selected="inventoryPool === pool"
                :class="{ active: inventoryPool === pool }"
                @click="inventoryPool = pool"
              >
                {{ poolLabel(pool) }} <span>{{ componentCount(pool) }}</span>
              </button>
            </div>

            <div v-if="inventoryMode === 'components' && inventoryStacks.length" class="component-grid">
              <button
                v-for="stack in inventoryStacks"
                :key="stack.key"
                type="button"
                class="component-card"
                :class="[`tier-${stack.component.tier_id}`, { selected: selectedComponentIDs[stack.component.pool_id] === stack.component.id, miracle: stack.component.is_jackpot, incompatible: !isComponentCompatible(stack.component) }]"
                :data-testid="`component-${stack.component.id}`"
                :data-stack-id="stack.key"
                :disabled="!isComponentCompatible(stack.component)"
                @click="selectComponent(stack.component)"
              >
                <span class="component-rarity">{{ stack.component.rarity }}</span>
                <img :src="componentAsset(stack.component)" :alt="stack.component.name">
                <strong>{{ stack.component.name }}</strong>
                <small>{{ stack.component.wool_value }} 羊毛</small>
                <span class="component-quantity">×{{ componentStackCount(stack.component) }}</span>
                <span class="component-check"><Icon name="check" size="xs" /></span>
              </button>
            </div>
            <div v-else-if="inventoryMode === 'components'" class="empty-panel compact-empty">
              <Icon :name="poolIcon(inventoryPool)" size="xl" />
              <strong>{{ poolLabel(inventoryPool) }}还没有组件</strong>
            </div>
            <div v-else-if="cards.items.length" class="card-vault-grid">
              <article
                v-for="card in cards.items"
                :key="card.id"
                class="card-vault-card"
                :class="[`tier-${cardTier(card)}`, { miracle: card.is_jackpot }]"
              >
                <div class="card-vault-visual">
                  <img :src="cardAsset(card)" :alt="card.title">
                  <span>{{ card.is_jackpot ? '奇迹' : '组合卡' }}</span>
                </div>
                <div class="card-vault-copy">
                  <h3>{{ card.title }}</h3>
                  <p>{{ card.breed_name }} · {{ card.profession_name }} · {{ card.treasure_name }}</p>
                  <footer>
                    <strong>{{ card.wool_value }} 羊毛</strong>
                    <button type="button" @click="openDialog('redeem')"><Icon name="dollar" size="xs" />兑换</button>
                  </footer>
                </div>
              </article>
            </div>
            <div v-else class="empty-panel compact-empty"><Icon name="sparkles" size="xl" /><strong>组合成功后会收进这里</strong></div>
          </div>

          <div v-else-if="activeDialog === 'collection'" class="dialog-panel collection-panel">
            <div class="panel-toolbar">
              <p>已发现 {{ overview.collection.length }} 种组合</p>
            </div>
            <div v-if="overview.collection.length" class="collection-grid">
              <article
                v-for="entry in overview.collection"
                :key="collectionKey(entry)"
                class="collection-card"
                :class="[`tier-${collectionTier(entry)}`, { matched: !!entry.recipe_id, miracle: entry.recipe_id === 'miracle' }]"
              >
                <div class="collection-visual">
                  <img :src="collectionAsset(entry)" :alt="entry.title"><span>×{{ entry.discovered_count }}</span>
                </div>
                <div>
                  <h3>{{ entry.title }}</h3>
                  <p>{{ entry.breed_name }} · {{ entry.profession_name }} · {{ entry.treasure_name }}</p>
                  <small>{{ entry.available_count }} 张可兑换 · {{ entry.wool_value }} 羊毛</small>
                </div>
              </article>
            </div>
            <div v-else class="empty-panel compact-empty"><Icon name="book" size="xl" /><strong>图鉴等待第一张成品卡</strong></div>
          </div>

          <div v-else-if="activeDialog === 'history'" class="dialog-panel history-panel">
            <div class="panel-toolbar"><p>共 {{ history.total }} 批召唤记录</p></div>
            <div v-if="history.items.length" class="history-list" role="list">
              <button
                v-for="batch in history.items"
                :key="batch.id"
                type="button"
                class="history-row"
                role="listitem"
                @click="openHistoryBatch(batch)"
              >
                <span class="history-icon"><Icon :name="poolIcon(normalizePool(batch.pool_id))" size="sm" /></span>
                <span class="history-main"><strong>{{ poolLabel(normalizePool(batch.pool_id)) }} · {{ batch.draw_count }} 连召唤</strong><small>{{ formatDate(batch.created_at) }}</small></span>
                <span class="history-preview">
                  <img v-for="component in batch.components.slice(0, 4)" :key="component.id" :src="componentAsset(component)" alt="">
                </span>
                <span class="history-wool">+{{ batch.total_wool }}</span>
                <Icon name="chevronRight" size="sm" />
              </button>
            </div>
            <div v-else class="empty-panel compact-empty"><Icon name="clock" size="xl" /><strong>暂无召唤记录</strong></div>
          </div>

          <div v-else-if="activeDialog === 'redeem'" class="dialog-panel redeem-panel">
            <div class="redeem-summary">
              <div><small>组件卡</small><strong>{{ components.total }} 张</strong></div>
              <div><small>组合卡</small><strong>{{ cards.total }} 张</strong></div>
              <div><small>最大可兑换</small><strong>${{ formatMoney(overview.wallet.available_reward) }}</strong></div>
            </div>

            <div v-if="latestRedemption" class="redemption-result">
              <span class="redemption-success"><Icon name="check" size="lg" /></span>
              <div><small>{{ latestRedemption.component_count + latestRedemption.card_count }} 张卡已兑换</small><strong>${{ formatMoney(latestRedemption.reward) }}</strong></div>
              <code data-testid="generated-redeem-code">{{ latestRedemption.redeem_code }}</code>
              <button type="button" data-testid="copy-redeem-code" @click="copyRedeemCode(latestRedemption.redeem_code)">
                <Icon name="copy" size="sm" />复制
              </button>
            </div>

            <p v-if="actionError" class="dialog-error" role="alert">{{ actionError }}</p>

            <label class="redeem-max-control">
              <input type="checkbox" :checked="redeemMaximum" data-testid="redeem-maximum" @change="toggleRedeemMaximum">
              <span><strong>最大可兑换</strong><small>当前全部 {{ components.total + cards.total }} 张卡</small></span>
              <em>${{ formatMoney(overview.wallet.available_reward) }}</em>
            </label>

            <div class="inventory-kind-tabs redeem-kind-tabs" role="tablist" aria-label="兑换卡牌类型">
              <button type="button" role="tab" :aria-selected="redeemMode === 'components'" :class="{ active: redeemMode === 'components' }" @click="redeemMode = 'components'">
                <Icon name="cube" size="sm" />组件卡<span>{{ components.total }}</span>
              </button>
              <button type="button" role="tab" :aria-selected="redeemMode === 'cards'" :class="{ active: redeemMode === 'cards' }" @click="redeemMode = 'cards'">
                <Icon name="sparkles" size="sm" />组合卡<span>{{ cards.total }}</span>
              </button>
            </div>

            <div v-if="redeemMode === 'components' && components.items.length" class="finished-grid">
              <label
                v-for="component in components.items"
                :key="component.id"
                class="finished-card"
                :class="[`tier-${component.tier_id}`, { selected: isRedeemComponentSelected(component.id), miracle: component.is_jackpot }]"
              >
                <input type="checkbox" :checked="isRedeemComponentSelected(component.id)" :aria-label="`选择 ${component.name}`" @change="toggleRedeemComponent(component.id)">
                <img :src="componentAsset(component)" :alt="component.name">
                <span><strong>{{ component.name }}</strong><small>{{ poolLabel(component.pool_id) }} · {{ component.wool_value }} 羊毛</small></span>
              </label>
            </div>
            <div v-else-if="redeemMode === 'cards' && cards.items.length" class="finished-grid">
              <label
                v-for="card in cards.items"
                :key="card.id"
                class="finished-card"
                :class="[`tier-${cardTier(card)}`, { selected: isRedeemCardSelected(card.id), miracle: card.is_jackpot }]"
              >
                <input type="checkbox" :checked="isRedeemCardSelected(card.id)" :aria-label="`选择 ${card.title}`" @change="toggleRedeemCard(card.id)">
                <img :src="cardAsset(card)" :alt="card.title">
                <span><strong>{{ card.title }}</strong><small>{{ card.wool_value }} 羊毛</small></span>
              </label>
            </div>
            <div v-else class="empty-panel compact-empty"><Icon name="cube" size="xl" /><strong>这一类暂时没有可兑换卡牌</strong></div>

            <div class="redeem-actions">
              <div><small>已选 {{ selectedRedeemCount }} 张</small><strong>${{ formatMoney(selectedReward) }}</strong></div>
              <button type="button" class="redeem-button" data-testid="redeem-selected" :disabled="!canRedeemSelected || redeeming" @click="redeemSelected">
                {{ selectedRedeemLabel }}
              </button>
            </div>

            <section class="code-history" aria-labelledby="code-history-title">
              <div class="code-history-heading"><h3 id="code-history-title">兑换记录</h3><span>{{ redemptions.total }} 条</span></div>
              <div v-if="redemptions.items.length" class="code-list">
                <article v-for="redemption in redemptions.items" :key="redemption.id">
                  <div><strong>${{ formatMoney(redemption.reward) }}</strong><small>{{ redemption.component_count + redemption.card_count }} 张 · {{ formatDate(redemption.created_at) }} · {{ redemptionStatus(redemption.status) }}</small></div>
                  <code>{{ redemption.redeem_code }}</code>
                  <button type="button" :aria-label="`复制记录 ${redemption.id} 的兑换码`" @click="copyRedeemCode(redemption.redeem_code)"><Icon name="copy" size="sm" /></button>
                </article>
              </div>
              <p v-else>暂无兑换记录</p>
            </section>
          </div>
        </BaseDialog>
      </template>

      <Transition name="toast">
        <div v-if="toast" class="reward-toast" role="status">{{ toast }}</div>
      </Transition>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import {
  combineRewardDrawComponents,
  drawRewardCards,
  getRewardDrawCards,
  getRewardDrawComponents,
  getRewardDrawHistory,
  getRewardDrawOverview,
  getRewardDrawRedemptions,
  redeemRewardDrawItems,
  type RewardDrawBatch,
  type RewardDrawCard,
  type RewardDrawCardPage,
  type RewardDrawCollectionEntry,
  type RewardDrawComponent,
  type RewardDrawComponentPage,
  type RewardDrawHistoryPage,
  type RewardDrawOverview,
  type RewardDrawPoolID,
  type RewardDrawRecipe,
  type RewardDrawRedemption,
  type RewardDrawRedemptionPage,
} from '@/api/rewardDraw'

type DialogID = 'inventory' | 'collection' | 'history' | 'redeem'
type CardKind = 'components' | 'cards'
type IconName = InstanceType<typeof Icon>['$props']['name']

interface PendingDraw {
  requestId: string
  count: number
  poolId: RewardDrawPoolID
  userId: number
}

interface RewardDrawComponentStack {
  key: string
  component: RewardDrawComponent
  items: RewardDrawComponent[]
}

const PENDING_DRAW_KEY = 'sub2api:reward-draw:pending-v3'
const poolIDs: RewardDrawPoolID[] = ['breed', 'profession', 'treasure']
const tierOrder = ['code', 'reasoning', 'speed', 'inspiration', 'wisdom', 'fortune', 'miracle']
const componentAssets: Record<string, string> = {
  'breed:code': '/reward-draw/generated/sheep-fluffy.webp',
  'breed:reasoning': '/reward-draw/generated/sheep-meadow.webp',
  'breed:speed': '/reward-draw/generated/sheep-knight.webp',
  'breed:inspiration': '/reward-draw/generated/sheep-fluffy.webp',
  'breed:wisdom': '/reward-draw/generated/sheep-scholar.webp',
  'breed:fortune': '/reward-draw/generated/sheep-miner.webp',
  'breed:miracle': '/reward-draw/generated/sheep-fluffy.webp',
  'profession:code': '/reward-draw/generated/profession-code.webp',
  'profession:reasoning': '/reward-draw/generated/profession-reasoning.webp',
  'profession:speed': '/reward-draw/generated/profession-speed.webp',
  'profession:inspiration': '/reward-draw/generated/profession-inspiration.webp',
  'profession:wisdom': '/reward-draw/generated/profession-wisdom.webp',
  'profession:fortune': '/reward-draw/generated/profession-fortune.webp',
  'profession:miracle': '/reward-draw/generated/profession-miracle.webp',
  'treasure:code': '/reward-draw/generated/treasure-code.webp',
  'treasure:reasoning': '/reward-draw/generated/treasure-reasoning.webp',
  'treasure:speed': '/reward-draw/generated/treasure-speed.webp',
  'treasure:inspiration': '/reward-draw/generated/treasure-inspiration.webp',
  'treasure:wisdom': '/reward-draw/generated/treasure-wisdom.webp',
  'treasure:fortune': '/reward-draw/generated/treasure-fortune.webp',
  'treasure:miracle': '/reward-draw/generated/treasure-miracle.webp',
}
const recipeAssets: Record<string, string> = Object.fromEntries(
  tierOrder.map((tier) => [tier, `/reward-draw/generated/card-${tier}.webp`]),
)

const overview = ref<RewardDrawOverview | null>(null)
const components = ref<RewardDrawComponentPage>({ items: [], total: 0, limit: 300, offset: 0 })
const cards = ref<RewardDrawCardPage>({ items: [], total: 0, limit: 300, offset: 0 })
const history = ref<RewardDrawHistoryPage>({ items: [], total: 0, limit: 30, offset: 0 })
const redemptions = ref<RewardDrawRedemptionPage>({ items: [], total: 0, limit: 30, offset: 0 })
const latestBatch = ref<RewardDrawBatch | null>(null)
const pendingBatch = ref<RewardDrawBatch | null>(null)
const revealBatch = ref<RewardDrawBatch | null>(null)
const latestRedemption = ref<RewardDrawRedemption | null>(null)
const activeDialog = ref<DialogID | null>(null)
const activePool = ref<RewardDrawPoolID>('breed')
const inventoryPool = ref<RewardDrawPoolID>('breed')
const inventoryMode = ref<CardKind>('components')
const redeemMode = ref<CardKind>('components')
const selectedComponentIDs = ref<Record<RewardDrawPoolID, number | null>>({ breed: null, profession: null, treasure: null })
const selectedRedeemComponentIDs = ref<number[]>([])
const selectedCardIDs = ref<number[]>([])
const redeemMaximum = ref(true)
const initialLoading = ref(true)
const loadError = ref(false)
const actionError = ref('')
const drawing = ref(false)
const revealing = ref(false)
const combining = ref(false)
const combineQuantity = ref(1)
const redeeming = ref(false)
const reducedMotion = ref(false)
const toast = ref('')
const { copyToClipboard } = useClipboard()

let revealTimer: ReturnType<typeof setTimeout> | undefined
let toastTimer: ReturnType<typeof setTimeout> | undefined
let motionQuery: MediaQueryList | undefined

const activePoolName = computed(() => poolLabel(activePool.value))
const drawAllCount = computed(() => Math.min(overview.value?.wallet.ticket_balance || 0, overview.value?.maximum_draw_count || 100))
const progressPercent = computed(() => Math.min(100, Math.max(0, Math.round((overview.value?.wallet.consumption_progress || 0) * 100))))
const featuredComponent = computed(() => latestBatch.value?.components[0] || null)
const recentComponents = computed(() => latestBatch.value?.components.slice(0, 5) || [])
const inventoryStacks = computed<RewardDrawComponentStack[]>(() => {
  const stacks = new Map<string, RewardDrawComponentStack>()
  for (const component of components.value.items) {
    if (component.pool_id !== inventoryPool.value) continue
    const key = `${component.pool_id}:${component.tier_id}`
    const stack = stacks.get(key)
    if (stack) stack.items.push(component)
    else stacks.set(key, { key, component, items: [component] })
  }
  return [...stacks.values()]
})
const selectedComponents = computed(() => poolIDs.map((pool) => selectedComponent(pool)).filter((component): component is RewardDrawComponent => !!component))
const currentRecipe = computed(() => {
  if (!selectedComponents.value.length) return null
  const recipeID = selectedComponents.value.some((component) => component.is_jackpot)
    ? 'miracle'
    : new Set(selectedComponents.value.map((component) => component.tier_id)).size === 1
      ? selectedComponents.value[0].tier_id
      : ''
  return overview.value?.recipes.find((recipe) => recipe.id === recipeID) || null
})
const combineMaximum = computed(() => selectedComponents.value.length === 3 && currentRecipe.value
  ? Math.min(...selectedComponents.value.map(componentStackCount))
  : 0)
const canCombine = computed(() => combineMaximum.value > 0 && combineQuantity.value >= 1 && combineQuantity.value <= combineMaximum.value)
const combinePreviewWool = computed(() => (currentRecipe.value?.wool || 0) * (canCombine.value ? combineQuantity.value : 1))
const currentRecipeHint = computed(() => {
  if (!selectedComponents.value.length) return '选择任一组件，组合台会匹配固定配方'
  if (!currentRecipe.value) return '这组组件没有固定配方'
  const missing = poolIDs.filter((pool) => !selectedComponent(pool)).map(poolLabel)
  return missing.length
    ? `${currentRecipe.value.name} · 还缺 ${missing.join('、')}`
    : `${recipeFormula(currentRecipe.value)} · 每张 ${currentRecipe.value.wool} 羊毛 · 最多 ${combineMaximum.value} 张`
})
const combineButtonLabel = computed(() => {
  if (combining.value) return `正在合成 ${combineQuantity.value} 张`
  if (canCombine.value) return `合成 ${combineQuantity.value} 张${currentRecipe.value?.name}`
  return selectedComponents.value.length === 3 ? '这组没有固定配方' : '还需选择三池组件'
})
watch(combineMaximum, (maximum) => setCombineQuantity(maximum))
const selectedWool = computed(() => {
  if (redeemMaximum.value) return overview.value?.wallet.available_wool || 0
  const selectedComponents = new Set(selectedRedeemComponentIDs.value)
  const selectedCards = new Set(selectedCardIDs.value)
  return components.value.items.reduce((total, component) => total + (selectedComponents.has(component.id) ? component.wool_value : 0), 0)
    + cards.value.items.reduce((total, card) => total + (selectedCards.has(card.id) ? card.wool_value : 0), 0)
})
const selectedRedeemCount = computed(() => redeemMaximum.value
  ? components.value.total + cards.value.total
  : selectedRedeemComponentIDs.value.length + selectedCardIDs.value.length)
const selectedReward = computed(() => selectedWool.value / (overview.value?.wool_per_credit || 1000))
const minimumRedemptionReward = computed(() => overview.value?.minimum_redemption_reward || 0.001)
const canRedeemSelected = computed(() => selectedRedeemCount.value > 0 && selectedReward.value >= minimumRedemptionReward.value)
const selectedRedeemLabel = computed(() => {
  if (redeeming.value) return '正在生成'
  if (!selectedRedeemCount.value) return '暂无可兑换卡牌'
  const gap = Math.max(0, minimumRedemptionReward.value - selectedReward.value)
  return gap > 0 ? `还差 $${formatMoney(gap)}` : `生成 $${formatMoney(selectedReward.value)} 兑换码`
})
const dialogTitle = computed(() => {
  if (revealBatch.value) return `召唤结算 · ${revealBatch.value.components.length} 张`
  return ({ inventory: '卡片图存', collection: '羊羊图鉴', history: '召唤历史', redeem: '兑换额度' } as const)[activeDialog.value || 'inventory']
})
const dialogWidth = computed<'normal' | 'wide' | 'extra-wide'>(() => {
  if (revealBatch.value) return revealBatch.value.components.length === 1 ? 'normal' : 'extra-wide'
  return activeDialog.value === 'redeem' ? 'wide' : 'extra-wide'
})

function poolIcon(pool: RewardDrawPoolID): IconName {
  return ({ breed: 'badge', profession: 'user', treasure: 'gift' } as const)[pool]
}

function poolHint(pool: RewardDrawPoolID): string {
  return ({ breed: '发现新品种', profession: '解锁新职业', treasure: '收集稀有宝物' } as const)[pool]
}

function poolLabel(pool: RewardDrawPoolID): string {
  return ({ breed: '羊种池', profession: '职业池', treasure: '宝物池' } as const)[pool]
}

function normalizePool(pool: RewardDrawBatch['pool_id']): RewardDrawPoolID {
  return pool === 'profession' || pool === 'treasure' ? pool : 'breed'
}

function componentAsset(component: RewardDrawComponent): string {
  return componentAssets[`${component.pool_id}:${component.tier_id}`] || componentAssets[`${component.pool_id}:code`]
}

function recipeAsset(tier: string): string {
  return recipeAssets[tier] || recipeAssets.code
}

function collectionAsset(entry: RewardDrawCollectionEntry): string {
  return recipeAsset(collectionTier(entry))
}

function cardAsset(card: RewardDrawCard): string {
  return recipeAsset(cardTier(card))
}

function recipeFormula(recipe: RewardDrawRecipe): string {
  if (recipe.id === 'miracle') return '任一奇迹组件 + 其余两池任意组件 → 奇迹咩'
  const names = poolIDs.map((pool) => overview.value?.pools.find((item) => item.id === pool)?.items.find((item) => item.tier_id === recipe.id)?.name || recipe.id)
  return `${names.join(' + ')} → ${recipe.name}`
}

function componentCount(pool: RewardDrawPoolID): number {
  const countedTiers = new Set<string>()
  let count = 0
  for (const component of components.value.items) {
    if (component.pool_id !== pool || countedTiers.has(component.tier_id)) continue
    countedTiers.add(component.tier_id)
    count += componentStackCount(component)
  }
  return count
}

function componentStackCount(component: RewardDrawComponent): number {
  const loadedCount = components.value.items.filter((item) => item.pool_id === component.pool_id && item.tier_id === component.tier_id).length
  return Math.max(component.stack_count || 0, loadedCount)
}

function selectedComponent(pool: RewardDrawPoolID): RewardDrawComponent | undefined {
  const id = selectedComponentIDs.value[pool]
  return components.value.items.find((component) => component.id === id)
}

function selectComponent(component: RewardDrawComponent): void {
  if (!isComponentCompatible(component)) return
  selectedComponentIDs.value = {
    ...selectedComponentIDs.value,
    [component.pool_id]: selectedComponentIDs.value[component.pool_id] === component.id ? null : component.id,
  }
}

function isComponentCompatible(component: RewardDrawComponent): boolean {
  const others = selectedComponents.value.filter((selected) => selected.pool_id !== component.pool_id)
  return component.is_jackpot || others.some((selected) => selected.is_jackpot) || others.every((selected) => selected.tier_id === component.tier_id)
}

function clearSelectedComponent(pool: RewardDrawPoolID): void {
  if (!selectedComponentIDs.value[pool]) return
  selectedComponentIDs.value = { ...selectedComponentIDs.value, [pool]: null }
}

function setCombineQuantity(quantity: number): void {
  combineQuantity.value = combineMaximum.value > 0
    ? Math.min(combineMaximum.value, Math.max(1, Math.trunc(quantity || 1)))
    : 1
}

function updateCombineQuantity(event: Event): void {
  const input = event.target as HTMLInputElement
  setCombineQuantity(input.valueAsNumber)
  input.value = String(combineQuantity.value)
}

function openDialog(dialog: DialogID): void {
  if (revealBatch.value) return
  actionError.value = ''
  if (dialog === 'redeem') selectMaximumRedemption()
  activeDialog.value = dialog
}

function openInventory(pool: RewardDrawPoolID): void {
  inventoryPool.value = pool
  openDialog('inventory')
}

function closeDialog(): void {
  revealBatch.value = null
  activeDialog.value = null
  actionError.value = ''
}

function openRevealInventory(): void {
  if (!revealBatch.value) return
  const pool = normalizePool(revealBatch.value.pool_id)
  revealBatch.value = null
  openInventory(pool)
}

function canDraw(count: number): boolean {
  return !drawing.value && (overview.value?.wallet.ticket_balance || 0) >= count
}

async function loadData(): Promise<void> {
  initialLoading.value = true
  loadError.value = false
  try {
    await refreshData()
    const pending = readPendingDraw()
    if (pending) void performDraw(pending)
  } catch {
    loadError.value = true
  } finally {
    initialLoading.value = false
  }
}

async function refreshData(): Promise<void> {
  const [overviewResult, componentResult, cardResult, historyResult, redemptionResult] = await Promise.all([
    getRewardDrawOverview(),
    getRewardDrawComponents(),
    getRewardDrawCards(),
    getRewardDrawHistory(),
    getRewardDrawRedemptions(),
  ])
  overview.value = overviewResult
  components.value = componentResult
  cards.value = cardResult
  history.value = historyResult
  redemptions.value = redemptionResult
  latestBatch.value = historyResult.items.find((batch) => batch.components.length > 0) || null
  for (const pool of poolIDs) {
    const id = selectedComponentIDs.value[pool]
    if (id && !componentResult.items.some((component) => component.id === id)) clearSelectedComponent(pool)
  }
  if (redeemMaximum.value) {
    selectedRedeemComponentIDs.value = componentResult.items.map((component) => component.id)
    selectedCardIDs.value = cardResult.items.map((card) => card.id)
  } else {
    selectedRedeemComponentIDs.value = selectedRedeemComponentIDs.value.filter((id) => componentResult.items.some((component) => component.id === id))
    selectedCardIDs.value = selectedCardIDs.value.filter((id) => cardResult.items.some((card) => card.id === id))
  }
}

function startDraw(count: number): void {
  const pending = readPendingDraw()
  if (pending) {
    void performDraw(pending)
    return
  }
  const request: PendingDraw = {
    requestId: createRequestID(), count, poolId: activePool.value, userId: overview.value!.wallet.user_id,
  }
  localStorage.setItem(PENDING_DRAW_KEY, JSON.stringify(request))
  void performDraw(request)
}

async function performDraw(request: PendingDraw): Promise<void> {
  if (drawing.value) return
  drawing.value = true
  revealing.value = true
  actionError.value = ''
  activePool.value = request.poolId
  try {
    const result = await drawRewardCards(request.count, request.poolId, request.requestId)
    await refreshData()
    pendingBatch.value = result.batch
    if (reducedMotion.value) finishReveal()
    else revealTimer = setTimeout(finishReveal, 900)
  } catch (error) {
    drawing.value = false
    revealing.value = false
    const status = Number((error as { status?: number })?.status || 0)
    if (status >= 400 && status < 500) localStorage.removeItem(PENDING_DRAW_KEY)
    actionError.value = status === 402
      ? '羊铃不足，本次没有扣除羊铃'
      : status >= 400 && status < 500
        ? '召唤请求已失效，请重新发起'
        : '召唤结果尚未确认，再次点击会恢复本次召唤'
  }
}

function finishReveal(): void {
  if (revealTimer) clearTimeout(revealTimer)
  revealTimer = undefined
  if (pendingBatch.value) {
    latestBatch.value = pendingBatch.value
    inventoryPool.value = normalizePool(pendingBatch.value.pool_id)
    activeDialog.value = null
    revealBatch.value = pendingBatch.value
    pendingBatch.value = null
    localStorage.removeItem(PENDING_DRAW_KEY)
  }
  drawing.value = false
  revealing.value = false
}

function revealDelay(index: number): string {
  return `${Math.min(index, 12) * 45}ms`
}

async function combineSelected(): Promise<void> {
  if (!canCombine.value || combining.value) return
  combining.value = true
  actionError.value = ''
  try {
    const result = await combineRewardDrawComponents(
      selectedComponents.value.map((component) => component.id),
      combineQuantity.value,
    )
    selectedComponentIDs.value = { breed: null, profession: null, treasure: null }
    await refreshData()
    inventoryMode.value = 'cards'
    showToast(`${result.quantity} 张${result.cards[0]?.title || '羊卡'}已组合，共 ${result.wool_total} 羊毛`)
  } catch (error) {
    const status = Number((error as { status?: number })?.status || 0)
    actionError.value = status === 409 ? '组件状态已变化，图存已经刷新' : '组合没有完成，请稍后重试'
    if (status === 409) await refreshData()
  } finally {
    combining.value = false
  }
}

function selectMaximumRedemption(): void {
  redeemMaximum.value = true
  selectedRedeemComponentIDs.value = components.value.items.map((component) => component.id)
  selectedCardIDs.value = cards.value.items.map((card) => card.id)
  redeemMode.value = cards.value.total > 0 ? 'cards' : 'components'
}

function toggleRedeemMaximum(event: Event): void {
  if ((event.target as HTMLInputElement).checked) selectMaximumRedemption()
  else {
    redeemMaximum.value = false
    selectedRedeemComponentIDs.value = []
    selectedCardIDs.value = []
  }
}

function beginExplicitRedemption(): void {
  if (!redeemMaximum.value) return
  redeemMaximum.value = false
  selectedRedeemComponentIDs.value = components.value.items.map((component) => component.id)
  selectedCardIDs.value = cards.value.items.map((card) => card.id)
}

function isRedeemComponentSelected(id: number): boolean {
  return redeemMaximum.value || selectedRedeemComponentIDs.value.includes(id)
}

function isRedeemCardSelected(id: number): boolean {
  return redeemMaximum.value || selectedCardIDs.value.includes(id)
}

function toggleRedeemComponent(id: number): void {
  beginExplicitRedemption()
  selectedRedeemComponentIDs.value = selectedRedeemComponentIDs.value.includes(id)
    ? selectedRedeemComponentIDs.value.filter((item) => item !== id)
    : [...selectedRedeemComponentIDs.value, id]
}

function toggleRedeemCard(id: number): void {
  beginExplicitRedemption()
  selectedCardIDs.value = selectedCardIDs.value.includes(id)
    ? selectedCardIDs.value.filter((item) => item !== id)
    : [...selectedCardIDs.value, id]
}

async function redeemSelected(): Promise<void> {
  if (!canRedeemSelected.value || redeeming.value) return
  await redeemItems(
    redeemMaximum.value ? [] : selectedRedeemComponentIDs.value,
    redeemMaximum.value ? [] : selectedCardIDs.value,
    redeemMaximum.value,
  )
}

async function redeemItems(componentIDs: number[], cardIDs: number[], all: boolean): Promise<void> {
  redeeming.value = true
  actionError.value = ''
  try {
    const result = await redeemRewardDrawItems(componentIDs, cardIDs, all)
    redeemMaximum.value = true
    selectedRedeemComponentIDs.value = []
    selectedCardIDs.value = []
    latestRedemption.value = result
    await refreshData()
    showToast(`已生成 $${formatMoney(result.reward)} 兑换码`)
  } catch (error) {
    const status = Number((error as { status?: number })?.status || 0)
    actionError.value = status === 409 ? '卡牌状态已变化，列表已经刷新' : '兑换没有完成，请稍后重试'
    if (status === 409) await refreshData()
  } finally {
    redeeming.value = false
  }
}

async function copyRedeemCode(code: string): Promise<void> {
  await copyToClipboard(code, '兑换码已复制')
}

function openHistoryBatch(batch: RewardDrawBatch): void {
  latestBatch.value = batch
  openInventory(normalizePool(batch.pool_id))
}

function readPendingDraw(): PendingDraw | null {
  try {
    const value = JSON.parse(localStorage.getItem(PENDING_DRAW_KEY) || 'null') as PendingDraw | null
    if (
      value
      && typeof value.requestId === 'string'
      && Number.isInteger(value.count)
      && value.count > 0
      && value.count <= 100
      && poolIDs.includes(value.poolId)
      && value.userId === overview.value?.wallet.user_id
    ) return value
    localStorage.removeItem(PENDING_DRAW_KEY)
    return null
  } catch {
    localStorage.removeItem(PENDING_DRAW_KEY)
    return null
  }
}

function createRequestID(): string {
  return crypto.randomUUID()
}

function cardTier(card: RewardDrawCard): string {
  if (card.is_jackpot) return 'miracle'
  if (card.recipe_id) return card.recipe_id
  return [card.breed_id, card.profession_id, card.treasure_id]
    .sort((a, b) => tierOrder.indexOf(b) - tierOrder.indexOf(a))[0] || 'code'
}

function collectionTier(entry: RewardDrawCollectionEntry): string {
  if (entry.recipe_id) return entry.recipe_id
  return [entry.breed_id, entry.profession_id, entry.treasure_id]
    .sort((a, b) => tierOrder.indexOf(b) - tierOrder.indexOf(a))[0] || 'code'
}

function collectionKey(entry: RewardDrawCollectionEntry): string {
  return `${entry.breed_id}:${entry.profession_id}:${entry.treasure_id}`
}

function redemptionStatus(status: string): string {
  return status === 'used' ? '已使用' : status === 'disabled' ? '已停用' : '未使用'
}

function formatMoney(value: number): string {
  return Number(value || 0).toFixed(3).replace(/\.?0+$/, '')
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

function showToast(message: string): void {
  toast.value = message
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value = '' }, 2600)
}

function handleMotionChange(event: MediaQueryListEvent): void {
  reducedMotion.value = event.matches
}

onMounted(() => {
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  reducedMotion.value = motionQuery.matches
  motionQuery.addEventListener('change', handleMotionChange)
  void loadData()
})

onBeforeUnmount(() => {
  if (revealTimer) clearTimeout(revealTimer)
  if (toastTimer) clearTimeout(toastTimer)
  motionQuery?.removeEventListener('change', handleMotionChange)
})
</script>

<style scoped>
:global(.reward-draw-layout) { background: #f2f3f5; }
:global(.reward-draw-layout main) { padding: 14px 12px 28px; }
:global(.reward-draw-layout main > div) { max-width: 1460px; }

.reward-page,
.dialog-panel {
  --canvas: #fbfaf7;
  --surface: #fff;
  --ink: #20201f;
  --muted: #726f6a;
  --line: #e5e0d8;
  --coral: #ff5b4f;
  --coral-dark: #d9473e;
  --cyan: #18a8bd;
  --cyan-dark: #0f7f91;
  --yellow: #ffc928;
  --green: #3d8f43;
  --tier: #6f716d;
}

.reward-page { color: var(--ink); display: grid; font-family: "Microsoft YaHei UI", "Microsoft YaHei", system-ui, sans-serif; gap: 14px; letter-spacing: 0; margin: 0 auto; max-width: 1370px; min-width: 0; }
.reward-page *, .reward-page *::before, .reward-page *::after, .dialog-panel *, .dialog-panel *::before, .dialog-panel *::after { box-sizing: border-box; letter-spacing: 0; }
.reward-page button, .dialog-panel button { align-items: center; border: 0; cursor: pointer; display: inline-flex; font: inherit; justify-content: center; }
.reward-page button:focus-visible, .dialog-panel button:focus-visible, .dialog-panel input:focus-visible { outline: 3px solid color-mix(in srgb, var(--cyan) 55%, white); outline-offset: 2px; }

.arcade-hero { background: var(--canvas); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 14px 38px rgb(37 31 24 / 6%); display: grid; gap: 14px; grid-template-columns: minmax(390px, 1.1fr) minmax(500px, .9fr); min-height: 670px; overflow: hidden; padding: 0 22px; position: relative; }
.intro-column { align-self: start; grid-column: 1; grid-row: 1; padding: 36px 0 28px 8px; position: relative; width: 225px; z-index: 2; }
.brand-lockup { align-items: center; display: flex; font-size: 20px; font-weight: 900; gap: 9px; }
.brand-lockup::before { background: var(--coral); border-radius: 4px; content: ""; height: 20px; width: 5px; }
.intro-column h1 { font-size: 48px; font-weight: 950; line-height: 1.04; margin: 34px 0 20px; }
.draw-rule { color: #464440; font-size: 15px; line-height: 1.7; margin: 0; }
.draw-rule strong { color: var(--coral-dark); font-size: 18px; }
.bell-progress { margin-top: 28px; width: 190px; }
.bell-progress > div { color: var(--muted); display: flex; font-size: 10px; justify-content: space-between; }
.bell-progress strong { color: var(--green); }
.progress-track { background: #e8e4dc; border-radius: 3px; display: block; height: 6px; margin-top: 7px; overflow: hidden; }
.progress-track i { background: var(--green); display: block; height: 100%; }

.machine-column { align-items: flex-end; display: flex; grid-column: 1; grid-row: 1; justify-content: center; min-width: 0; padding-left: 126px; padding-top: 14px; position: relative; z-index: 1; }
.machine-visual { align-items: center; display: flex; justify-content: center; max-width: 560px; min-width: 0; position: relative; width: 100%; }
.machine-art { aspect-ratio: 1; display: block; max-height: 635px; max-width: none; object-fit: contain; width: 164%; }
.machine-column.is-revealing .machine-art { animation: machine-rattle 170ms ease-in-out infinite alternate; }
.machine-readout { align-items: center; background: #24211e; border: 1px solid #a68a5d; border-radius: 5px; bottom: 27px; color: #fff; display: grid; gap: 2px 10px; grid-template-columns: auto minmax(0, 1fr) auto; left: 50%; max-width: 320px; min-height: 48px; padding: 7px 12px; position: absolute; transform: translateX(-50%); width: 70%; }
.machine-readout span { color: #d5cfc3; font-size: 10px; }
.machine-readout strong { font-size: 12px; overflow: hidden; text-align: center; text-overflow: ellipsis; white-space: nowrap; }
.machine-readout em { color: var(--yellow); font-size: 11px; font-style: normal; font-weight: 800; white-space: nowrap; }
.skip-button { background: #24211e; border-radius: 6px; bottom: 20px; color: #fff; padding: 8px 12px; position: absolute; right: 0; z-index: 3; }

.control-console { align-self: center; background: rgb(255 255 255 / 94%); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 14px 34px rgb(37 31 24 / 7%); grid-column: 2; grid-row: 1; min-width: 0; padding: 28px; position: relative; width: 100%; z-index: 2; }
.console-heading { align-items: center; display: flex; justify-content: space-between; margin-bottom: 20px; }
.console-heading > div:first-child { border-left: 4px solid var(--green); padding-left: 12px; }
.console-heading small { color: var(--muted); display: block; font-size: 11px; }
.console-heading h2 { font-size: 23px; font-weight: 800; margin: 3px 0 0; }
.ticket-balance { align-items: center; background: #24211e; border-radius: 8px; color: #fff; display: flex; gap: 7px; min-height: 52px; padding: 0 17px; }
.ticket-balance svg { color: var(--yellow); }
.ticket-balance span { font-size: 24px; font-weight: 900; }
.ticket-balance small { color: #c8c2b8; }
.pool-selector { display: grid; gap: 12px; grid-template-columns: repeat(3, minmax(0, 1fr)); }
.pool-selector button { background: #fff; border: 1px solid #ddd7ce; border-radius: 8px; color: var(--ink); display: grid; gap: 7px; min-height: 150px; min-width: 0; padding: 19px 8px 15px; position: relative; transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease; }
.pool-selector button:hover { border-color: var(--coral); transform: translateY(-2px); }
.pool-selector button.active { background: #fff7f5; border: 2px solid var(--coral); box-shadow: 0 8px 18px rgb(255 91 79 / 10%); padding: 18px 7px 14px; }
.pool-selector button > svg { height: 34px; width: 34px; }
.pool-selector strong { font-size: 17px; white-space: nowrap; }
.pool-selector small { color: var(--muted); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; width: 100%; }
.selected-mark { align-items: center; background: var(--coral); border-radius: 0 7px 0 8px; color: #fff; display: flex; height: 29px; justify-content: center; position: absolute; right: -2px; top: -2px; width: 29px; }
.draw-actions { display: grid; gap: 11px 12px; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 20px; }
.draw-button { border-radius: 8px; color: #fff; gap: 10px; min-height: 76px; padding: 0 18px; transition: box-shadow 160ms ease, transform 160ms ease; }
.draw-button span { font-size: 16px; font-weight: 800; }
.draw-button strong { margin-left: auto; white-space: nowrap; }
.draw-cost { align-items: center; display: flex; gap: 4px; }
.draw-cost svg { color: var(--yellow); }
.draw-button:disabled { cursor: not-allowed; filter: grayscale(.55); opacity: .48; }
.draw-button:not(:disabled):hover { box-shadow: 0 9px 20px rgb(30 26 22 / 13%); transform: translateY(-2px); }
.draw-once { background: var(--coral); }
.draw-ten { background: var(--cyan); }
.draw-all { background: var(--yellow); color: #262016; grid-column: 1 / -1; }
.sync-note { align-items: center; color: var(--muted); display: flex; font-size: 12px; gap: 7px; line-height: 1.45; margin: 14px 0 0; }
.console-tools { border-top: 1px dashed var(--line); display: grid; gap: 8px; grid-template-columns: repeat(4, minmax(0, 1fr)); margin-top: 36px; padding-top: 16px; }
.console-tools button { background: #f8f6f2; border: 1px solid transparent; border-radius: 8px; color: var(--ink); display: grid; gap: 7px; min-height: 66px; min-width: 0; padding: 8px 4px; }
.console-tools button:hover { background: #fff7f5; border-color: #ffd0cb; color: var(--coral-dark); }
.console-tools span { font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; width: 100%; }

.inline-error, .dialog-error { align-items: center; background: #fff1ee; border: 1px solid #efb8b1; border-radius: 6px; color: #98372e; display: flex; font-size: 12px; gap: 7px; margin: 0; padding: 10px 12px; }
.recent-collection { background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 10px 26px rgb(37 31 24 / 4%); min-width: 0; padding: 16px; }
.section-heading { align-items: center; display: flex; justify-content: space-between; margin-bottom: 12px; min-height: 28px; }
.section-heading > div { align-items: center; display: flex; gap: 8px; }
.section-heading h2 { font-size: 14px; margin: 0; }
.section-heading button { background: transparent; color: var(--muted); font-size: 11px; gap: 3px; padding: 5px; }
.status-dot { background: var(--green); border-radius: 50%; height: 8px; width: 8px; }
.showcase-track { display: grid; gap: 10px; grid-template-columns: repeat(5, minmax(0, 1fr)); }
.showcase-card { background: #fff; border: 1px solid var(--line) !important; border-radius: 8px; color: var(--ink); display: grid !important; min-width: 0; padding: 8px; position: relative; text-align: left; transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease; }
.showcase-card:hover { border-color: var(--tier) !important; box-shadow: 0 8px 18px rgb(37 31 24 / 7%); transform: translateY(-2px); }
.showcase-card img { aspect-ratio: 1.18; object-fit: contain; width: 100%; }
.showcase-card strong { font-size: 12px; margin-top: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; width: 100%; }
.showcase-card small { color: var(--muted); font-size: 10px; margin-top: 2px; }
.rarity-tag, .component-rarity { background: var(--tier); border-radius: 3px; color: #fff; font-size: 8px; font-weight: 900; padding: 2px 4px; position: absolute; z-index: 2; }
.rarity-tag { left: 6px; top: 6px; }
.recent-empty { align-items: center; color: var(--muted); display: flex; font-size: 11px; justify-content: center; min-height: 130px; }

.dialog-panel {
  --surface: var(--reward-dialog-surface, #fff);
  --ink: var(--reward-dialog-ink, #20201f);
  --muted: var(--reward-dialog-muted, #726f6a);
  --line: var(--reward-dialog-line, #e5e0d8);
  --coral: var(--reward-dialog-coral, #ff5b4f);
  --coral-dark: var(--reward-dialog-coral-dark, #d9473e);
  --cyan: var(--reward-dialog-cyan, #18a8bd);
  --cyan-dark: var(--reward-dialog-cyan-dark, #0f7f91);
  --green: var(--reward-dialog-green, #3d8f43);
  --panel-bg: var(--reward-dialog-bg, #fff);
  --panel-raised: var(--reward-dialog-raised, #fff);
  --panel-soft: var(--reward-dialog-soft, #faf8f4);
  --panel-subtle: var(--reward-dialog-subtle, #f7f5f0);
  --panel-hover: var(--reward-dialog-hover, #faf7f2);
  --panel-accent: var(--reward-dialog-accent, #fff7f5);
  --panel-success: var(--reward-dialog-success, #eef8ef);
  --panel-success-border: var(--reward-dialog-success-border, #b8d9bb);
  --panel-success-ink: var(--reward-dialog-success-ink, #315d38);
  --panel-dark: var(--reward-dialog-dark, #24211e);
  background: var(--panel-bg);
  color: var(--ink);
  font-family: "Microsoft YaHei UI", "Microsoft YaHei", system-ui, sans-serif;
  min-width: 0;
}
:global(.dark) {
  --reward-dialog-surface: #263244;
  --reward-dialog-ink: #f3f4f6;
  --reward-dialog-muted: #b8c0cc;
  --reward-dialog-line: #3b485a;
  --reward-dialog-coral: #ff746a;
  --reward-dialog-coral-dark: #ff9a93;
  --reward-dialog-cyan: #57c4d2;
  --reward-dialog-cyan-dark: #82d4de;
  --reward-dialog-green: #83ce8d;
  --reward-dialog-bg: #1e293b;
  --reward-dialog-raised: #263244;
  --reward-dialog-soft: #172131;
  --reward-dialog-subtle: #243142;
  --reward-dialog-hover: #2b394c;
  --reward-dialog-accent: #3b2c35;
  --reward-dialog-success: #193529;
  --reward-dialog-success-border: #386049;
  --reward-dialog-success-ink: #b3e6bb;
  --reward-dialog-dark: #0f172a;
}
.draw-result-panel { display: grid; gap: 18px; }
.reveal-summary { align-items: center; animation: settlement-in 320ms cubic-bezier(.2, .85, .25, 1) both; background: var(--panel-subtle); border: 1px solid var(--line); border-radius: 8px; display: grid; gap: 14px; grid-template-columns: 52px minmax(0, 1fr) auto; padding: 14px 16px; }
.reveal-complete-icon { align-items: center; background: var(--yellow); border-radius: 7px; color: #272119; display: flex; height: 52px; justify-content: center; width: 52px; }
.reveal-summary > div { display: grid; gap: 3px; min-width: 0; }
.reveal-summary small { color: var(--muted); font-size: 11px; }
.reveal-summary strong { font-size: 16px; overflow-wrap: anywhere; }
.reveal-summary > div > span { color: var(--muted); font-size: 12px; line-height: 1.5; }
.reveal-wool { border-left: 1px solid var(--line); padding-left: 20px; text-align: right; }
.reveal-wool strong { color: var(--green); font-size: 22px; }
.result-grid { display: grid; gap: 12px; grid-template-columns: repeat(5, minmax(0, 1fr)); max-height: min(510px, calc(90dvh - 292px)); overflow-y: auto; padding: 2px; }
.result-grid.single { grid-template-columns: minmax(0, 250px); justify-content: center; max-height: none; overflow: visible; }
.result-card { animation: result-card-in 420ms cubic-bezier(.2, .85, .25, 1) both; background: var(--panel-raised); border: 1px solid var(--line); border-bottom: 3px solid var(--tier); border-radius: 8px; display: grid; min-width: 0; padding: 9px; position: relative; }
.result-card.miracle { box-shadow: 0 0 0 2px color-mix(in srgb, var(--yellow) 52%, transparent), 0 10px 24px rgb(217 71 62 / 16%); }
.result-card-art { align-items: center; aspect-ratio: 1; background: var(--panel-soft); border-radius: 6px; display: flex; justify-content: center; overflow: hidden; }
.result-card-art img { height: 100%; object-fit: contain; width: 100%; }
.result-card > strong { font-size: 13px; margin-top: 8px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.result-card > small { color: color-mix(in srgb, var(--tier) 76%, var(--ink)); font-size: 11px; font-weight: 800; margin-top: 2px; }
.result-rarity { background: var(--tier); border-radius: 3px; color: #fff; font-size: 10px; font-weight: 900; left: 15px; padding: 2px 6px; position: absolute; top: 15px; z-index: 1; }
.result-grid.single .result-card > strong { font-size: 15px; }
.result-grid.single .result-card > small { font-size: 12px; }
.reveal-actions { border-top: 1px solid var(--line); display: flex; gap: 10px; justify-content: flex-end; padding-top: 16px; }
.reveal-actions button { font-size: 13px; min-height: 42px; }
.reveal-inventory-button { background: var(--coral); border-radius: 7px; color: #fff; gap: 6px; padding: 0 16px; }
.panel-toolbar { align-items: center; display: flex; gap: 18px; justify-content: space-between; margin-bottom: 18px; }
.panel-toolbar p { color: var(--muted); font-size: 13px; line-height: 1.5; margin: 0; }
.secondary-button, .redeem-button, .combine-button { border-radius: 7px; gap: 6px; min-height: 42px; padding: 0 15px; }
.secondary-button { background: var(--panel-raised); border: 1px solid var(--line) !important; color: var(--ink); }
.secondary-button.compact { font-size: 12px; }
.redeem-button { background: var(--coral); color: #fff; font-size: 13px; font-weight: 800; }
.secondary-button:disabled, .redeem-button:disabled, .combine-button:disabled { cursor: not-allowed; opacity: .58; }

.inventory-kind-tabs { display: grid; gap: 8px; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-bottom: 18px; }
.inventory-kind-tabs button { background: var(--panel-soft); border: 1px solid var(--line); border-radius: 7px; color: var(--muted); gap: 7px; min-height: 44px; }
.inventory-kind-tabs button span { align-items: center; background: var(--panel-raised); border: 1px solid var(--line); border-radius: 4px; display: inline-flex; font-size: 11px; height: 22px; justify-content: center; min-width: 28px; }
.inventory-kind-tabs button.active { background: var(--panel-accent); border-color: var(--coral); color: var(--coral-dark); font-weight: 800; }

.compose-board { background: var(--panel-soft); border-bottom: 1px solid var(--line); border-top: 1px solid var(--line); margin: 0 -24px 20px; padding: 20px 24px; }
.compose-heading { align-items: end; display: flex; gap: 16px; justify-content: space-between; margin-bottom: 14px; }
.compose-heading small { color: var(--green); font-size: 11px; font-weight: 800; }
.compose-heading h3 { font-size: 17px; margin: 3px 0 0; }
.compose-heading > strong { color: var(--green); font-size: 20px; white-space: nowrap; }
.recipe-hint { align-items: center; background: var(--panel-success); border-left: 3px solid var(--green); color: var(--panel-success-ink); display: flex; font-size: 12px; gap: 8px; line-height: 1.5; margin: 0 0 12px; min-height: 40px; padding: 9px 11px; }
.compose-slots { display: grid; gap: 12px; grid-template-columns: repeat(3, minmax(0, 1fr)); }
.compose-slot { background: var(--panel-raised); border: 1px dashed color-mix(in srgb, var(--muted) 56%, transparent) !important; border-radius: 8px; color: var(--muted); display: grid !important; gap: 5px 8px; grid-template-columns: 54px minmax(0, 1fr); grid-template-rows: 1fr 1fr; justify-content: initial !important; min-height: 82px; min-width: 0; padding: 9px; text-align: left; }
.compose-slot > svg { align-self: center; grid-row: 1 / -1; justify-self: center; }
.compose-slot img { align-self: center; grid-row: 1 / -1; height: 60px; object-fit: contain; width: 54px; }
.compose-slot span { align-self: end; color: var(--ink); font-size: 13px; font-weight: 800; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.compose-slot small { align-self: start; font-size: 11px; }
.compose-slot.filled { border-style: solid !important; border-color: var(--cyan) !important; }
.combine-quantity-row { align-items: center; display: flex; gap: 16px; justify-content: space-between; margin-top: 12px; }
.combine-quantity-row label { display: grid; gap: 2px; }
.combine-quantity-row label strong { font-size: 13px; }
.combine-quantity-row label small { color: var(--muted); font-size: 11px; }
.quantity-stepper { display: grid; grid-template-columns: 40px 72px 40px 58px; height: 40px; }
.quantity-stepper button, .quantity-stepper input { background: var(--panel-raised); border: 1px solid var(--line); border-radius: 0; color: var(--ink); height: 40px; min-width: 0; }
.quantity-stepper button { padding: 0; }
.quantity-stepper button:first-child { border-radius: 7px 0 0 7px; }
.quantity-stepper .quantity-maximum { border-radius: 0 7px 7px 0; color: var(--green); font-size: 12px; font-weight: 800; }
.quantity-stepper input { border-inline-width: 0; font-size: 14px; font-weight: 800; padding: 0 4px; text-align: center; width: 72px; }
.quantity-stepper button:disabled { cursor: not-allowed; opacity: .45; }
.combine-button { background: var(--panel-dark); color: #fff; font-size: 13px; font-weight: 800; margin-top: 12px; width: 100%; }
.recipe-guide { border-top: 1px solid var(--line); margin-top: 14px; padding-top: 12px; }
.recipe-guide summary { color: var(--muted); cursor: pointer; font-size: 12px; font-weight: 800; list-style-position: inside; }
.recipe-list { display: grid; gap: 0 18px; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 9px; }
.recipe-row { align-items: center; border-top: 1px solid var(--line); display: grid; gap: 10px; grid-template-columns: 42px minmax(0, 1fr) auto; min-height: 56px; padding: 6px 0; }
.recipe-row img { height: 42px; object-fit: contain; width: 42px; }
.recipe-row span { font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
.recipe-row strong { color: color-mix(in srgb, var(--tier) 76%, var(--ink)); font-size: 12px; white-space: nowrap; }
.pool-tabs { display: grid; gap: 8px; grid-template-columns: repeat(3, minmax(0, 1fr)); margin-bottom: 14px; }
.pool-tabs button { background: var(--panel-soft); border: 1px solid transparent; border-radius: 7px; color: var(--muted); font-size: 13px; gap: 7px; min-height: 44px; }
.pool-tabs button span { align-items: center; background: var(--panel-raised); border: 1px solid var(--line); border-radius: 4px; display: flex; font-size: 11px; height: 22px; justify-content: center; min-width: 26px; }
.pool-tabs button.active { background: var(--panel-accent); border-color: var(--coral); color: var(--coral-dark); font-weight: 800; }
.component-grid { display: grid; gap: 12px; grid-template-columns: repeat(5, minmax(0, 1fr)); }
.component-card { background: var(--panel-raised); border: 1px solid var(--line) !important; border-radius: 8px; color: var(--ink); display: grid !important; min-height: 204px; min-width: 0; padding: 9px; position: relative; text-align: left; }
.component-card:hover, .component-card.selected { border-color: var(--tier) !important; box-shadow: 0 7px 16px rgb(37 31 24 / 7%); }
.component-card.selected { box-shadow: inset 0 0 0 1px var(--tier); }
.component-card.incompatible { box-shadow: none; cursor: not-allowed; opacity: .42; }
.component-card img { aspect-ratio: 1; object-fit: contain; width: 100%; }
.component-card strong { font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; width: 100%; }
.component-card > small { color: color-mix(in srgb, var(--tier) 76%, var(--ink)); font-size: 11px; font-weight: 800; margin-top: 4px; padding-right: 34px; }
.component-rarity { font-size: 10px; left: 8px; padding: 2px 6px; top: 8px; }
.component-quantity { align-items: center; background: var(--panel-dark); border: 1px solid var(--panel-raised); border-radius: 4px; bottom: 8px; color: #fff; display: flex; font-size: 11px; font-weight: 800; height: 24px; justify-content: center; min-width: 30px; padding: 0 5px; position: absolute; right: 8px; }
.component-check { align-items: center; background: var(--tier); border-radius: 4px; color: #fff; display: none; height: 24px; justify-content: center; position: absolute; right: 8px; top: 8px; width: 24px; }
.component-card.selected .component-check { display: flex; }

.card-vault-grid { display: grid; gap: 12px; grid-template-columns: repeat(3, minmax(0, 1fr)); }
.card-vault-card { background: var(--panel-raised); border: 1px solid var(--line); border-top: 3px solid var(--tier); border-radius: 8px; display: grid; gap: 12px; grid-template-columns: 104px minmax(0, 1fr); min-height: 148px; padding: 10px; }
.card-vault-card.miracle { box-shadow: 0 0 0 2px color-mix(in srgb, var(--yellow) 48%, transparent); }
.card-vault-visual { align-items: center; background: var(--panel-soft); border-radius: 6px; display: flex; justify-content: center; min-height: 122px; overflow: hidden; position: relative; }
.card-vault-visual img { height: 112px; object-fit: contain; width: 100%; }
.card-vault-visual span { background: var(--tier); border-radius: 3px; color: #fff; font-size: 10px; font-weight: 800; left: 7px; padding: 2px 5px; position: absolute; top: 7px; }
.card-vault-copy { display: grid; min-width: 0; }
.card-vault-copy h3 { font-size: 14px; margin: 3px 0 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.card-vault-copy p { color: var(--muted); font-size: 11px; line-height: 1.5; margin: 0; overflow-wrap: anywhere; }
.card-vault-copy footer { align-items: end; display: flex; gap: 8px; justify-content: space-between; margin-top: 8px; }
.card-vault-copy footer strong { color: color-mix(in srgb, var(--tier) 76%, var(--ink)); font-size: 12px; white-space: nowrap; }
.card-vault-copy footer button { background: var(--panel-dark); border-radius: 6px; color: #fff; font-size: 11px; gap: 4px; min-height: 34px; padding: 0 9px; }

.collection-grid { display: grid; gap: 12px; grid-template-columns: repeat(3, minmax(0, 1fr)); }
.collection-card { align-items: center; background: var(--panel-raised); border: 1px solid var(--line); border-left: 3px solid var(--tier); border-radius: 8px; display: grid; gap: 13px; grid-template-columns: 88px minmax(0, 1fr); min-height: 122px; padding: 11px 13px; }
.collection-visual { align-items: center; background: var(--panel-soft); border-radius: 6px; display: flex; height: 94px; justify-content: center; position: relative; }
.collection-visual img { height: 90px; object-fit: contain; width: 84px; }
.collection-visual span { background: var(--panel-raised); border: 1px solid var(--line); border-radius: 4px; bottom: -3px; font-size: 11px; padding: 2px 5px; position: absolute; right: -3px; }
.collection-card h3 { font-size: 14px; margin: 0 0 6px; }
.collection-card p { color: var(--muted); font-size: 12px; margin: 0 0 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.collection-card small { color: color-mix(in srgb, var(--tier) 76%, var(--ink)); font-size: 11px; font-weight: 700; }

.history-list { background: var(--panel-raised); border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
.history-row { background: transparent; color: var(--ink); display: grid !important; gap: 14px; grid-template-columns: 40px minmax(170px, 1fr) minmax(140px, auto) minmax(64px, auto) 18px; justify-content: initial !important; min-height: 72px; padding: 10px 16px; text-align: left; width: 100%; }
.history-row + .history-row { border-top: 1px solid var(--line); }
.history-row:hover { background: var(--panel-hover); }
.history-icon { align-items: center; background: var(--panel-accent); border-radius: 7px; color: var(--coral); display: flex; height: 38px; justify-content: center; width: 38px; }
.history-main { display: grid; }
.history-main strong { font-size: 13px; }
.history-main small { color: var(--muted); font-size: 11px; }
.history-preview { display: flex; justify-content: flex-end; }
.history-preview img { background: var(--panel-soft); border: 1px solid var(--panel-raised); border-radius: 5px; height: 40px; margin-left: -6px; object-fit: contain; width: 40px; }
.history-wool { color: var(--green); font-size: 13px; font-weight: 800; justify-self: end; }

.redeem-summary { background: var(--panel-soft); border-bottom: 1px solid var(--line); border-top: 1px solid var(--line); display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0 -24px 18px; padding: 0 24px; }
.redeem-summary > div { display: grid; gap: 5px; padding: 16px; text-align: center; }
.redeem-summary > div + div { border-left: 1px solid var(--line); }
.redeem-summary small { color: var(--muted); font-size: 12px; }
.redeem-summary strong { color: var(--green); font-size: 20px; }
.redemption-result { align-items: center; background: var(--panel-success); border: 1px solid var(--panel-success-border); border-radius: 8px; display: grid; gap: 12px; grid-template-columns: 42px minmax(110px, auto) minmax(0, 1fr) auto; margin-bottom: 18px; padding: 12px; }
.redemption-success { align-items: center; background: var(--panel-raised); border-radius: 7px; color: var(--green); display: flex; height: 42px; justify-content: center; width: 42px; }
.redemption-result > div { display: grid; }
.redemption-result small { color: var(--muted); font-size: 11px; }
.redemption-result > div strong { color: var(--green); font-size: 19px; }
.redemption-result code, .code-list code { font-family: Consolas, "Courier New", monospace; font-size: 12px; overflow-wrap: anywhere; }
.redemption-result button { background: var(--panel-dark); border-radius: 6px; color: #fff; font-size: 12px; gap: 5px; min-height: 40px; padding: 0 12px; }
.redeem-max-control { align-items: center; background: var(--panel-success); border: 1px solid var(--panel-success-border); border-radius: 8px; cursor: pointer; display: grid; gap: 11px; grid-template-columns: 18px minmax(0, 1fr) auto; margin-bottom: 12px; min-height: 58px; padding: 10px 13px; }
.redeem-max-control input { accent-color: var(--green); height: 17px; margin: 0; width: 17px; }
.redeem-max-control span { display: grid; gap: 2px; }
.redeem-max-control strong { color: var(--panel-success-ink); font-size: 13px; }
.redeem-max-control small { color: var(--muted); font-size: 11px; }
.redeem-max-control em { color: var(--green); font-size: 18px; font-style: normal; font-weight: 900; }
.redeem-kind-tabs { margin-bottom: 12px; }
.finished-grid { display: grid; gap: 10px; grid-template-columns: repeat(3, minmax(0, 1fr)); max-height: 350px; overflow-y: auto; padding: 1px; }
.finished-card { align-items: center; background: var(--panel-raised); border: 1px solid var(--line); border-radius: 8px; cursor: pointer; display: grid; gap: 10px; grid-template-columns: 18px 58px minmax(0, 1fr); min-height: 80px; padding: 9px; }
.finished-card.selected { border-color: var(--tier); box-shadow: inset 0 0 0 1px var(--tier); }
.finished-card input { accent-color: var(--coral); height: 16px; margin: 0; width: 16px; }
.finished-card img { height: 60px; object-fit: contain; width: 58px; }
.finished-card > span { display: grid; gap: 3px; min-width: 0; }
.finished-card strong { font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.finished-card small { color: color-mix(in srgb, var(--tier) 76%, var(--ink)); font-size: 11px; }
.redeem-actions { align-items: center; border-bottom: 1px solid var(--line); border-top: 1px solid var(--line); display: grid; gap: 10px; grid-template-columns: minmax(120px, 1fr) auto; margin-top: 18px; padding: 14px 0; }
.redeem-actions > div { display: grid; }
.redeem-actions small { color: var(--muted); font-size: 11px; }
.redeem-actions strong { color: var(--green); font-size: 20px; }
.code-history { margin-top: 18px; }
.code-history-heading { align-items: center; display: flex; justify-content: space-between; margin-bottom: 10px; }
.code-history-heading h3 { font-size: 15px; margin: 0; }
.code-history-heading span, .code-history > p { color: var(--muted); font-size: 11px; }
.code-list { background: var(--panel-raised); border: 1px solid var(--line); border-radius: 8px; max-height: 240px; overflow-y: auto; }
.code-list article { align-items: center; display: grid; gap: 12px; grid-template-columns: 100px minmax(0, 1fr) 38px; min-height: 64px; padding: 10px 12px; }
.code-list article + article { border-top: 1px solid var(--line); }
.code-list article > div { display: grid; }
.code-list strong { color: var(--green); font-size: 13px; }
.code-list small { color: var(--muted); font-size: 10px; }
.code-list button { background: var(--panel-soft); border-radius: 6px; color: var(--ink); height: 36px; width: 36px; }

.tier-code { --tier: #6f716d; }
.tier-reasoning { --tier: #3e72ba; }
.tier-speed { --tier: #168d7f; }
.tier-inspiration { --tier: #c85d6f; }
.tier-wisdom { --tier: #8d4bb3; }
.tier-fortune { --tier: #bd7e00; }
.tier-miracle { --tier: #d9473e; }
.miracle { border-color: var(--coral) !important; box-shadow: inset 0 0 0 1px var(--yellow); }
.empty-panel, .state-panel { align-items: center; background: var(--panel-soft, #fcfaf6); border: 1px dashed var(--line); border-radius: 8px; color: var(--muted); display: flex; flex-direction: column; justify-content: center; min-height: 230px; padding: 24px; text-align: center; }
.empty-panel > svg { color: #aaa39a; margin-bottom: 10px; }
.empty-panel strong, .state-panel strong { color: var(--ink); font-size: 14px; }
.compact-empty { min-height: 160px; }
.state-panel { min-height: 540px; }
.state-panel .secondary-button { margin-top: 14px; }
.error-state > svg { color: var(--coral); margin-bottom: 8px; }
.loading-ring { animation: loading-spin 800ms linear infinite; border: 2px solid var(--line); border-radius: 50%; border-top-color: var(--coral); height: 24px; margin-bottom: 10px; width: 24px; }
.reward-toast { background: #24211e; border-radius: 7px; bottom: 24px; color: #fff; font-size: 11px; left: 50%; max-width: calc(100vw - 32px); padding: 10px 14px; position: fixed; transform: translateX(-50%); z-index: 100; }
.toast-enter-active, .toast-leave-active { transition: opacity 160ms ease, transform 160ms ease; }
.toast-enter-from, .toast-leave-to { opacity: 0; transform: translate(-50%, 7px); }

@keyframes machine-rattle { from { transform: rotate(-.45deg) translateX(-1px); } to { transform: rotate(.45deg) translateX(1px); } }
@keyframes settlement-in { from { opacity: 0; transform: translateY(-8px); } to { opacity: 1; transform: translateY(0); } }
@keyframes result-card-in { from { opacity: 0; transform: translateY(16px) scale(.92); } to { opacity: 1; transform: translateY(0) scale(1); } }
@keyframes loading-spin { to { transform: rotate(360deg); } }

@media (min-width: 1280px) {
  .machine-visual { max-width: 640px; }
  .machine-art { scale: 1.087; }
}

@media (max-width: 1279px) {
  .arcade-hero { grid-template-columns: minmax(180px, .42fr) minmax(310px, .58fr); min-height: 0; padding-inline: 16px; }
  .intro-column { grid-column: 1; grid-row: 1; width: auto; }
  .machine-column { grid-column: 2; grid-row: 1; padding-left: 0; }
  .machine-art { max-height: 470px; width: 100%; }
  .control-console { grid-column: 1 / -1; grid-row: 2; margin: 0 auto 16px; max-width: 720px; }
}

@media (max-width: 900px) {
  .showcase-track { overflow-x: auto; padding-bottom: 6px; scroll-snap-type: x mandatory; }
  .showcase-card { min-width: 122px; scroll-snap-align: start; }
  .component-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .card-vault-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .collection-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .finished-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 680px) {
  :global(.reward-draw-layout main) { padding: 8px; }
  .reward-page { gap: 9px; }
  .arcade-hero { gap: 0; grid-template-columns: 1fr; min-height: 0; padding: 0 10px 12px; }
  .intro-column { grid-column: 1; grid-row: 1; padding: 18px 5px 0; width: auto; }
  .brand-lockup { font-size: 17px; }
  .intro-column h1 { font-size: 34px; margin: 18px 0 10px; }
  .draw-rule { font-size: 13px; }
  .bell-progress { margin-top: 14px; }
  .machine-column { grid-column: 1; grid-row: 2; margin-top: -50px; padding-left: 64px; pointer-events: none; }
  .machine-art { max-height: 390px; width: 100%; }
  .machine-readout { bottom: 15px; width: 78%; }
  .skip-button { pointer-events: auto; right: 3px; }
  .control-console { grid-column: 1; grid-row: 3; margin: 0; padding: 13px; }
  .console-heading h2 { font-size: 16px; }
  .ticket-balance { min-height: 40px; }
  .pool-selector { gap: 6px; }
  .pool-selector button { min-height: 94px; padding-inline: 4px; }
  .pool-selector button.active { padding-inline: 3px; }
  .pool-selector button > svg { height: 24px; width: 24px; }
  .pool-selector strong { font-size: 12px; }
  .pool-selector small { display: none; }
  .draw-actions { gap: 6px; margin-top: 10px; }
  .draw-button { gap: 5px; min-height: 54px; padding: 0 8px; }
  .draw-button span { font-size: 12px; }
  .draw-button strong { font-size: 11px; }
  .console-tools { gap: 5px; margin-top: 12px; padding-top: 10px; }
  .console-tools button { gap: 4px; min-height: 56px; }
  .console-tools span { font-size: 9px; }
  .recent-collection { padding: 11px; }
  .showcase-track { display: flex; }
  .panel-toolbar { align-items: flex-start; display: grid; gap: 9px; }
  .compose-board { margin-inline: -16px; padding: 18px 16px; }
  .compose-slots { gap: 7px; }
  .compose-slot { grid-template-columns: 1fr; grid-template-rows: 43px auto auto; min-height: 112px; padding: 7px 5px; text-align: center; }
  .compose-slot > svg, .compose-slot img { grid-row: 1; height: 42px; justify-self: center; width: 42px; }
  .compose-slot span, .compose-slot small { justify-self: center; max-width: 100%; }
  .combine-quantity-row { align-items: stretch; flex-direction: column; gap: 8px; }
  .quantity-stepper { grid-template-columns: 40px minmax(64px, 1fr) 40px 58px; width: 100%; }
  .quantity-stepper input { width: 100%; }
  .recipe-list { grid-template-columns: 1fr; }
  .reveal-summary { grid-template-columns: 44px minmax(0, 1fr); padding: 12px; }
  .reveal-complete-icon { height: 44px; width: 44px; }
  .reveal-wool { border-left: 0; grid-column: 2; padding-left: 0; text-align: left; }
  .result-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); max-height: min(400px, calc(90dvh - 340px)); }
  .result-grid.single { grid-template-columns: minmax(0, 230px); }
  .reveal-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .reveal-actions button { width: 100%; }
  .component-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .card-vault-grid { grid-template-columns: 1fr; }
  .collection-grid { grid-template-columns: 1fr; }
  .history-row { gap: 8px; grid-template-columns: 38px minmax(90px, 1fr) auto 16px; padding: 9px; }
  .history-preview { display: none; }
  .finished-grid { grid-template-columns: 1fr; }
  .redeem-summary { margin-inline: -16px; padding-inline: 16px; }
  .redemption-result { grid-template-columns: 42px minmax(0, 1fr) auto; }
  .redemption-result code { grid-column: 1 / -1; }
  .redeem-actions { align-items: stretch; grid-template-columns: 1fr; }
  .redeem-actions button { width: 100%; }
  .code-list article { grid-template-columns: 84px minmax(0, 1fr) 36px; }
}

@media (max-width: 400px) {
  .machine-column { padding-left: 50px; }
  .machine-readout { grid-template-columns: 1fr; text-align: center; }
  .machine-readout span, .machine-readout em { display: none; }
  .draw-button > svg { display: none; }
  .redeem-summary { grid-template-columns: 1fr; }
  .redeem-summary > div + div { border-left: 0; border-top: 1px solid var(--line); }
}

@media (prefers-reduced-motion: reduce) {
  .machine-column.is-revealing .machine-art, .loading-ring { animation-duration: 1ms; }
  .reveal-summary, .result-card { animation: none; }
  .pool-selector button, .draw-button, .showcase-card, .toast-enter-active, .toast-leave-active { transition-duration: 1ms; }
  .pool-selector button:hover, .draw-button:not(:disabled):hover, .showcase-card:hover { transform: none; }
}
</style>
