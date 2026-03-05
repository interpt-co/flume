<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useLogsStore } from '../stores/logs'
import { useSettingsStore } from '../stores/settings'
import { useConnectionStore } from '../stores/connection'
import LogRow from './LogRow.vue'
import type { LogMessage } from '../types'

const props = defineProps<{
  selectedMessage?: LogMessage | null
}>()

defineEmits<{
  'row-click': [message: LogMessage]
}>()

const logsStore = useLogsStore()
const settings = useSettingsStore()
const connectionStore = useConnectionStore()

const container = ref<HTMLElement | null>(null)
const showFollowButton = ref(false)
let trimTimer: ReturnType<typeof setTimeout> | null = null

function onScroll() {
  if (!container.value) return
  const { scrollTop, scrollHeight, clientHeight } = container.value
  const atBottom = scrollHeight - scrollTop - clientHeight < 30
  const atTop = scrollTop < 30

  if (atBottom) {
    showFollowButton.value = false
    if (!settings.autoFollow) {
      resumeAutoFollow()
    }
  } else {
    if (settings.autoFollow) {
      settings.setAutoFollow(false)
      cancelTrimTimer()
    }
    showFollowButton.value = true

    if (atTop && logsStore.canLoadMore && !logsStore.isLoadingHistory) {
      loadHistory()
    }
  }
}

async function loadHistory() {
  if (!container.value) return
  const prevScrollHeight = container.value.scrollHeight

  await connectionStore.loadMoreHistory()

  await nextTick()
  if (container.value) {
    const newScrollHeight = container.value.scrollHeight
    container.value.scrollTop += newScrollHeight - prevScrollHeight
  }
}

function scrollToBottom() {
  if (!container.value) return
  container.value.scrollTop = container.value.scrollHeight
}

function resumeAutoFollow() {
  settings.setAutoFollow(true)
  showFollowButton.value = false
  nextTick(() => scrollToBottom())

  cancelTrimTimer()
  trimTimer = setTimeout(() => {
    if (settings.autoFollow) {
      logsStore.trimToWindow()
    }
    trimTimer = null
  }, 10000)
}

function cancelTrimTimer() {
  if (trimTimer) {
    clearTimeout(trimTimer)
    trimTimer = null
  }
}

watch(
  () => logsStore.messages.length,
  async () => {
    if (settings.autoFollow) {
      if (logsStore.messages.length > 600) {
        logsStore.trimToWindow()
      }
      await nextTick()
      scrollToBottom()
    }
  },
)

onMounted(() => {
  if (settings.autoFollow) {
    nextTick(() => scrollToBottom())
  }
})

onUnmounted(() => {
  cancelTrimTimer()
})
</script>

<template>
  <div class="log-viewer-wrapper">
    <div class="log-viewer" ref="container" @scroll="onScroll">
      <div v-if="logsStore.isLoadingHistory" class="log-viewer__loading">
        Loading history...
      </div>
      <div v-if="!logsStore.isLoadingHistory && logsStore.filteredMessages.length === 0" class="log-viewer__empty">
        <div class="log-viewer__empty-dot"></div>
        <div class="log-viewer__empty-title">Waiting for log messages...</div>
        <div class="log-viewer__empty-subtitle">Connect a log source to get started</div>
      </div>
      <LogRow
        v-for="msg in logsStore.filteredMessages"
        :key="msg.id"
        :message="msg"
        :selected="props.selectedMessage?.id === msg.id"
        @click="$emit('row-click', msg)"
      />
    </div>
    <Transition name="fade">
      <button
        v-if="showFollowButton"
        class="log-viewer__follow-btn"
        @click="resumeAutoFollow"
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
        Auto-follow
      </button>
    </Transition>
  </div>
</template>

<style scoped>
.log-viewer-wrapper {
  position: relative;
  display: flex;
  flex-direction: column;
}

.log-viewer {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  background-color: var(--flume-bg);
}

.log-viewer__loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  font-family: var(--flume-font-family);
  font-size: 11px;
  color: var(--flume-fg-secondary);
  opacity: 0.7;
}

.log-viewer__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--flume-fg-secondary);
  font-family: var(--flume-font-family);
  gap: 8px;
}

.log-viewer__empty-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background-color: var(--flume-fg-secondary);
  opacity: 0.4;
  animation: empty-pulse 2s ease-in-out infinite;
  margin-bottom: 4px;
}

@keyframes empty-pulse {
  0%, 100% { opacity: 0.15; transform: scale(1); }
  50% { opacity: 0.5; transform: scale(1.2); }
}

.log-viewer__empty-title {
  font-size: var(--flume-font-size);
}

.log-viewer__empty-subtitle {
  font-size: 11px;
  opacity: 0.5;
}

.log-viewer__follow-btn {
  position: absolute;
  bottom: 16px;
  right: 16px;
  display: flex;
  align-items: center;
  gap: 6px;
  background-color: var(--flume-accent);
  color: #fff;
  border: none;
  border-radius: 20px;
  padding: 7px 16px 7px 12px;
  font-family: var(--flume-font-family);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.35);
  z-index: 10;
  transition: opacity 0.15s, transform 0.15s;
}

.log-viewer__follow-btn:hover {
  opacity: 0.9;
  transform: translateY(-1px);
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
</style>
