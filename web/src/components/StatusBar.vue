<script setup lang="ts">
import { computed } from 'vue'
import { useConnectionStore } from '../stores/connection'
import { useLogsStore } from '../stores/logs'
import { usePatternsStore } from '../stores/patterns'
const connectionStore = useConnectionStore()
const logsStore = useLogsStore()
const patternsStore = usePatternsStore()

const statusLabel = computed(() => {
  switch (connectionStore.status) {
    case 'connected':
      return 'Connected'
    case 'connecting':
      return 'Connecting...'
    default:
      return 'Disconnected'
  }
})

const statusDotClass = computed(() => {
  return `status-bar__dot--${connectionStore.status}`
})

const messageCount = computed(() => logsStore.messages.length)

const bufferUsage = computed(() => {
  const stats = connectionStore.serverStats
  if (!stats) return null
  return `${stats.buffer_used}/${stats.buffer_capacity}`
})
</script>

<template>
  <div class="status-bar">
    <div class="status-bar__item">
      <span class="status-bar__dot" :class="statusDotClass"></span>
      <span>{{ statusLabel }}</span>
    </div>
    <div class="status-bar__divider"></div>
    <div class="status-bar__item">
      <span>Messages: {{ messageCount }}</span>
    </div>
    <div v-if="patternsStore.current" class="status-bar__divider"></div>
    <div v-if="patternsStore.current" class="status-bar__item">
      <span>Pattern: {{ patternsStore.current }}</span>
    </div>
    <div v-if="bufferUsage" class="status-bar__divider"></div>
    <div v-if="bufferUsage" class="status-bar__item">
      <span>Buffer: {{ bufferUsage }}</span>
    </div>
    <div class="status-bar__divider"></div>
    <div class="status-bar__item">
      <button @click="connectionStore.toggleFollowing()" class="status-bar__control" :title="connectionStore.following ? 'Pause' : 'Resume'">
        <svg v-if="connectionStore.following" width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/></svg>
        <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><polygon points="6,4 20,12 6,20"/></svg>
      </button>
    </div>
    <div class="status-bar__spacer"></div>
    <div v-if="!connectionStore.following" class="status-bar__item status-bar__item--subtle status-bar__item--paused">
      PAUSED
    </div>
  </div>
</template>

<style scoped>
.status-bar {
  display: flex;
  align-items: center;
  height: 34px;
  padding: 0 12px;
  background-color: var(--flume-bg-secondary);
  border-top: 1px solid var(--flume-border);
  font-family: var(--flume-font-family);
  font-size: 12px;
  color: var(--flume-fg-secondary);
  flex-shrink: 0;
  gap: 0;
  border-radius: 8px;
  margin: 0 6px 6px;
}

.status-bar__item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 8px;
}

.status-bar__item--subtle {
  opacity: 0.6;
  font-size: 10px;
  letter-spacing: 0.5px;
}

.status-bar__divider {
  width: 1px;
  height: 14px;
  background-color: var(--flume-border);
}

.status-bar__spacer {
  flex: 1;
}

.status-bar__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-bar__dot--connected {
  background-color: #22c55e;
}

.status-bar__dot--connecting {
  background-color: #eab308;
  animation: pulse-dot 1.5s ease-in-out infinite;
}

@keyframes pulse-dot {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.status-bar__dot--disconnected {
  background-color: #ef4444;
}

.status-bar__control {
  background: none;
  border: 1px solid var(--flume-border);
  color: var(--flume-fg-secondary);
  font-family: var(--flume-font-family);
  font-size: 11px;
  padding: 3px 10px;
  border-radius: 6px;
  cursor: pointer;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
}

.status-bar__control:hover {
  background-color: var(--flume-bg-hover);
  color: var(--flume-fg);
}

.status-bar__item--paused {
  color: #eab308;
}
</style>
