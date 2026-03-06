<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import type { LogMessage, WSMessage, ClientJoinedData, LogBulkData, StatusData } from './types'

// ---- Props ---------------------------------------------------------------

const props = withDefaults(
  defineProps<{
    wsUrl?: string
    theme?: 'light' | 'dark'
    autoFollow?: boolean
    showSearch?: boolean
    height?: string
  }>(),
  {
    wsUrl: '',
    theme: 'dark',
    autoFollow: true,
    showSearch: true,
    height: '400px',
  },
)

// ---- Emits ---------------------------------------------------------------

const emit = defineEmits<{
  'flume:connected': [clientId: string]
  'flume:disconnected': []
  'flume:message': [message: LogMessage]
  'flume:error': [error: string]
  'flume:row-click': [message: LogMessage]
}>()

// ---- Local state (replaces Pinia stores) ---------------------------------

// logs state
const messages = ref<LogMessage[]>([])
const seenIds = new Set<string>()
const filter = ref('')
const filterMode = ref<'text' | 'regex'>('text')
const isLoadingHistory = ref(false)

// connection state
const connStatus = ref<'connecting' | 'connected' | 'disconnected'>('disconnected')
const serverStats = ref<StatusData | null>(null)
const following = ref(props.autoFollow)

// settings state (local, no localStorage in WC context)
const currentTheme = ref<'light' | 'dark'>(props.theme)
const localAutoFollow = ref(props.autoFollow)
const showTimestamp = ref(true)
const showLevel = ref(true)
const showSource = ref(true)

// ---- Computed ------------------------------------------------------------

const filteredMessages = computed(() => {
  if (!filter.value) return messages.value

  if (filterMode.value === 'regex') {
    try {
      const re = new RegExp(filter.value, 'i')
      return messages.value.filter((msg) => re.test(msg.content))
    } catch {
      return messages.value
    }
  }

  const term = filter.value.toLowerCase()
  return messages.value.filter((msg) => msg.content.toLowerCase().includes(term))
})

const filterActive = computed(() => !!filter.value)
const matchCount = computed(() => filteredMessages.value.length)
const totalCount = computed(() => messages.value.length)

const regexMode = computed(() => filterMode.value === 'regex')

const statusLabel = computed(() => {
  switch (connStatus.value) {
    case 'connected': return 'Connected'
    case 'connecting': return 'Connecting...'
    default: return 'Disconnected'
  }
})

const statusDotClass = computed(() => `status-bar__dot--${connStatus.value}`)

const bufferUsage = computed(() => {
  const stats = serverStats.value
  if (!stats) return null
  return `${stats.buffer_used}/${stats.buffer_capacity}`
})

const levelClass = (msg: LogMessage): string => {
  const level = msg.level?.toLowerCase() ?? ''
  if (level.includes('fatal')) return 'log-row__level--fatal'
  if (level.includes('error') || level.includes('err')) return 'log-row__level--error'
  if (level.includes('warn')) return 'log-row__level--warn'
  if (level.includes('info')) return 'log-row__level--info'
  if (level.includes('debug') || level.includes('dbg') || level.includes('trace')) return 'log-row__level--debug'
  return ''
}

const formattedTime = (ts: string): string => {
  try {
    const d = new Date(ts)
    const h = String(d.getHours()).padStart(2, '0')
    const m = String(d.getMinutes()).padStart(2, '0')
    const s = String(d.getSeconds()).padStart(2, '0')
    const ms = String(d.getMilliseconds()).padStart(3, '0')
    return `${h}:${m}:${s}.${ms}`
  } catch {
    return ts
  }
}

// ---- WebSocket -----------------------------------------------------------

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectDelay = 1000
const MAX_RECONNECT_DELAY = 30000
let manualDisconnect = false

function connect() {
  const url = props.wsUrl
  if (!url) {
    emit('flume:error', 'wsUrl prop is required')
    return
  }

  if (ws) {
    ws.onclose = null
    ws.close()
    ws = null
  }

  manualDisconnect = false
  connStatus.value = 'connecting'
  ws = new WebSocket(url)

  ws.onopen = () => {
    reconnectDelay = 1000
  }

  ws.onmessage = (event: MessageEvent) => {
    try {
      const msg = JSON.parse(event.data) as WSMessage
      handleMessage(msg)
    } catch {
      // ignore malformed messages
    }
  }

  ws.onclose = () => {
    connStatus.value = 'disconnected'
    emit('flume:disconnected')
    if (!manualDisconnect) {
      scheduleReconnect()
    }
  }

  ws.onerror = () => {
    emit('flume:error', 'WebSocket connection error')
  }
}

function handleMessage(msg: WSMessage) {
  switch (msg.type) {
    case 'client_joined': {
      const data = msg.data as ClientJoinedData
      connStatus.value = 'connected'
      emit('flume:connected', data.client_id)
      break
    }
    case 'log_bulk': {
      const data = msg.data as LogBulkData
      addMessages(data.messages)
      break
    }
    case 'status': {
      const data = msg.data as StatusData
      serverStats.value = data
      break
    }
  }
}

function scheduleReconnect() {
  if (reconnectTimer) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY)
    connect()
  }, reconnectDelay)
}

function disconnect() {
  manualDisconnect = true
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (ws) {
    ws.onclose = null
    ws.close()
    ws = null
  }
  connStatus.value = 'disconnected'
  emit('flume:disconnected')
}

function send(msg: unknown) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(msg))
  }
}

function pause() {
  send({ type: 'set_status', data: { status: 'stopped' } })
  following.value = false
  localAutoFollow.value = false
}

function resume() {
  send({ type: 'set_status', data: { status: 'following' } })
  following.value = true
  localAutoFollow.value = true
}

function toggleFollowing() {
  if (following.value) {
    pause()
  } else {
    resume()
  }
}

// ---- Log management ------------------------------------------------------

const MAX_MESSAGES = 5000

function addMessages(msgs: LogMessage[]) {
  const newMsgs = msgs.filter((m) => !seenIds.has(m.id))
  if (newMsgs.length === 0) return

  for (const m of newMsgs) {
    seenIds.add(m.id)
    emit('flume:message', m)
  }

  messages.value.push(...newMsgs)

  if (messages.value.length > MAX_MESSAGES) {
    const excess = messages.value.length - MAX_MESSAGES
    const removed = messages.value.slice(0, excess)
    messages.value = messages.value.slice(excess)
    for (const m of removed) seenIds.delete(m.id)
  }
}

function clearMessages() {
  messages.value = []
  seenIds.clear()
}

// ---- Scroll / auto-follow ------------------------------------------------

const logContainer = ref<HTMLElement | null>(null)
const showFollowButton = ref(false)
let trimTimer: ReturnType<typeof setTimeout> | null = null

function onScroll() {
  if (!logContainer.value) return
  const { scrollTop, scrollHeight, clientHeight } = logContainer.value
  const atBottom = scrollHeight - scrollTop - clientHeight < 30

  if (atBottom) {
    showFollowButton.value = false
    if (!localAutoFollow.value) {
      resumeAutoFollow()
    }
  } else {
    if (localAutoFollow.value) {
      localAutoFollow.value = false
      cancelTrimTimer()
    }
    showFollowButton.value = true
  }
}

function scrollToBottom() {
  if (!logContainer.value) return
  logContainer.value.scrollTop = logContainer.value.scrollHeight
}

function resumeAutoFollow() {
  localAutoFollow.value = true
  showFollowButton.value = false
  nextTick(() => scrollToBottom())

  cancelTrimTimer()
  trimTimer = setTimeout(() => {
    if (localAutoFollow.value && messages.value.length > 500) {
      const removed = messages.value.slice(0, messages.value.length - 500)
      messages.value = messages.value.slice(messages.value.length - 500)
      for (const m of removed) seenIds.delete(m.id)
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
  () => messages.value.length,
  async () => {
    if (localAutoFollow.value) {
      if (messages.value.length > 600) {
        const removed = messages.value.slice(0, messages.value.length - 500)
        messages.value = messages.value.slice(messages.value.length - 500)
        for (const m of removed) seenIds.delete(m.id)
      }
      await nextTick()
      scrollToBottom()
    }
  },
)

// ---- Search bar ----------------------------------------------------------

const searchInput = ref<HTMLInputElement | null>(null)
const searchText = ref('')
const searchFocused = ref(false)
let debounceTimer: ReturnType<typeof setTimeout> | null = null

watch(searchText, (val) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    filter.value = val
  }, 200)
})

function toggleRegex() {
  filterMode.value = regexMode.value ? 'text' : 'regex'
}

function clearSearch() {
  searchText.value = ''
  filter.value = ''
}

// ---- Selected row --------------------------------------------------------

const selectedMessage = ref<LogMessage | null>(null)

function onRowClick(msg: LogMessage) {
  selectedMessage.value = msg
  emit('flume:row-click', msg)
}

// ---- Prop watchers -------------------------------------------------------

watch(() => props.theme, (val) => {
  currentTheme.value = val
})

watch(() => props.autoFollow, (val) => {
  following.value = val
  localAutoFollow.value = val
})

// ---- Lifecycle -----------------------------------------------------------

onMounted(() => {
  if (props.wsUrl) {
    connect()
  }
  if (localAutoFollow.value) {
    nextTick(() => scrollToBottom())
  }
})

onUnmounted(() => {
  disconnect()
  cancelTrimTimer()
  if (debounceTimer) clearTimeout(debounceTimer)
})

// ---- Public API (defineExpose) ------------------------------------------

defineExpose({
  connect,
  disconnect,
  pause,
  resume,
  clear: clearMessages,
  setTheme(t: 'light' | 'dark') {
    currentTheme.value = t
  },
  setFilter(text: string) {
    searchText.value = text
    filter.value = text
  },
})
</script>

<template>
  <div
    class="ilv-root"
    :data-theme="currentTheme"
    :style="{ height: props.height }"
  >
    <!-- Search bar -->
    <div
      v-if="props.showSearch"
      class="search-bar"
      :class="{ 'search-bar--focused': searchFocused }"
    >
      <div class="search-bar__icon">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="8"/>
          <line x1="21" y1="21" x2="16.65" y2="16.65"/>
        </svg>
      </div>
      <input
        ref="searchInput"
        v-model="searchText"
        class="search-bar__input"
        placeholder="Filter logs..."
        @keydown.escape="clearSearch"
        @focus="searchFocused = true"
        @blur="searchFocused = false"
      />
      <span v-if="filterActive" class="search-bar__count">
        {{ matchCount }} of {{ totalCount }}
      </span>
      <button
        class="search-bar__btn"
        :class="{ 'search-bar__btn--active': regexMode }"
        @click="toggleRegex"
        title="Toggle regex mode"
      >.*</button>
      <button v-if="searchText" class="search-bar__btn" @click="clearSearch" title="Clear">&#x2715;</button>
    </div>

    <!-- Log viewer -->
    <div class="log-viewer-wrapper">
      <div class="log-viewer" ref="logContainer" @scroll="onScroll">
        <div v-if="isLoadingHistory" class="log-viewer__loading">
          Loading history...
        </div>
        <div
          v-if="!isLoadingHistory && filteredMessages.length === 0"
          class="log-viewer__empty"
        >
          <div class="log-viewer__empty-dot"></div>
          <div class="log-viewer__empty-title">Waiting for log messages...</div>
          <div class="log-viewer__empty-subtitle">Connect a log source to get started</div>
        </div>
        <div
          v-for="msg in filteredMessages"
          :key="msg.id"
          class="log-row"
          :class="{ 'log-row--selected': selectedMessage?.id === msg.id }"
          @click="onRowClick(msg)"
        >
          <span v-if="showTimestamp" class="log-row__timestamp">{{ formattedTime(msg.ts) }}</span>
          <span
            v-if="showLevel && msg.level"
            class="log-row__level"
            :class="levelClass(msg)"
          >{{ msg.level.toUpperCase() }}</span>
          <span v-if="showSource" class="log-row__source">{{ msg.source }}</span>
          <span class="log-row__content" :class="{ 'log-row__content--json': msg.is_json }">{{ msg.content }}</span>
        </div>
      </div>
      <Transition name="fade">
        <button
          v-if="showFollowButton"
          class="log-viewer__follow-btn"
          @click="resumeAutoFollow"
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6 9 12 15 18 9"/>
          </svg>
          Auto-follow
        </button>
      </Transition>
    </div>

    <!-- Status bar -->
    <div class="status-bar">
      <div class="status-bar__item">
        <span class="status-bar__dot" :class="statusDotClass"></span>
        <span>{{ statusLabel }}</span>
      </div>
      <div class="status-bar__divider"></div>
      <div class="status-bar__item">
        <span>Messages: {{ totalCount }}</span>
      </div>
      <div v-if="bufferUsage" class="status-bar__divider"></div>
      <div v-if="bufferUsage" class="status-bar__item">
        <span>Buffer: {{ bufferUsage }}</span>
      </div>
      <div class="status-bar__divider"></div>
      <div class="status-bar__item">
        <button @click="toggleFollowing" class="status-bar__control" :title="following ? 'Pause' : 'Resume'">
          <svg v-if="following" width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
            <rect x="6" y="4" width="4" height="16" rx="1"/>
            <rect x="14" y="4" width="4" height="16" rx="1"/>
          </svg>
          <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
            <polygon points="6,4 20,12 6,20"/>
          </svg>
        </button>
      </div>
      <div class="status-bar__spacer"></div>
      <div v-if="!following" class="status-bar__item status-bar__item--subtle status-bar__item--paused">
        PAUSED
      </div>
    </div>
  </div>
</template>

<style>
/*
 * All styles are inlined here so they are injected into the shadow DOM.
 * CSS custom properties are declared under :host so they work inside the shadow root.
 * The data-theme attribute is set on the root element inside the shadow DOM.
 */

/* Reset */
*, *::before, *::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

/* Theme tokens — light (default) */
:host,
.ilv-root,
.ilv-root[data-theme="light"] {
  --flume-bg: #ffffff;
  --flume-bg-secondary: #f5f5f5;
  --flume-fg: #1a1a1a;
  --flume-fg-secondary: #666666;
  --flume-accent: #2563eb;
  --flume-border: #e5e5e5;
  --flume-font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
  --flume-font-size: 13px;
  --flume-row-height: 24px;
  --flume-level-debug: #6b7280;
  --flume-level-info: #2563eb;
  --flume-level-warn: #d97706;
  --flume-level-error: #dc2626;
  --flume-level-fatal: #7c2d12;
  --flume-bg-hover: #ebebeb;
  --flume-bg-active: #e0e7ff;
  --flume-shadow: rgba(0, 0, 0, 0.08);
  --flume-text-accent: #1d4ed8;
}

/* Theme tokens — dark */
.ilv-root[data-theme="dark"] {
  --flume-bg: #0f0f0f;
  --flume-bg-secondary: #1a1a1a;
  --flume-fg: #e5e5e5;
  --flume-fg-secondary: #999999;
  --flume-accent: #3b82f6;
  --flume-border: #333333;
  --flume-level-debug: #9ca3af;
  --flume-level-info: #60a5fa;
  --flume-level-warn: #fbbf24;
  --flume-level-error: #f87171;
  --flume-level-fatal: #fb923c;
  --flume-bg-hover: #252525;
  --flume-bg-active: #1e293b;
  --flume-shadow: rgba(0, 0, 0, 0.3);
  --flume-text-accent: #93c5fd;
}

/* Root layout */
.ilv-root {
  display: flex;
  flex-direction: column;
  background-color: var(--flume-bg);
  color: var(--flume-fg);
  font-family: var(--flume-font-family);
  overflow: hidden;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

/* Scrollbar */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}
::-webkit-scrollbar-track {
  background: var(--flume-bg);
}
::-webkit-scrollbar-thumb {
  background: var(--flume-border);
  border-radius: 4px;
}
::-webkit-scrollbar-thumb:hover {
  background: var(--flume-fg-secondary);
}

/* Search bar */
.search-bar {
  display: flex;
  align-items: center;
  height: 32px;
  padding: 0 8px;
  border-bottom: 1px solid var(--flume-border);
  border-left: 2px solid transparent;
  background-color: var(--flume-bg-secondary);
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
  flex-shrink: 0;
  gap: 6px;
  transition: border-color 0.15s;
}

.search-bar--focused {
  border-left-color: var(--flume-accent);
}

.search-bar__icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  color: var(--flume-fg-secondary);
  opacity: 0.5;
}

.search-bar__input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--flume-fg);
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
  padding: 4px 0;
}

.search-bar__input::placeholder {
  color: var(--flume-fg-secondary);
  opacity: 0.6;
}

.search-bar__count {
  flex-shrink: 0;
  color: var(--flume-fg-secondary);
  font-size: 11px;
  padding: 0 4px;
}

.search-bar__btn {
  flex-shrink: 0;
  background: none;
  border: 1px solid var(--flume-border);
  color: var(--flume-fg-secondary);
  font-family: var(--flume-font-family);
  font-size: 11px;
  padding: 1px 8px;
  border-radius: 3px;
  cursor: pointer;
  line-height: 1;
}

.search-bar__btn:hover {
  background-color: var(--flume-bg-hover);
  color: var(--flume-fg);
}

.search-bar__btn--active {
  border-color: var(--flume-accent);
  color: var(--flume-accent);
  background-color: transparent;
}

.search-bar__btn--active:hover {
  background-color: var(--flume-accent);
  color: var(--flume-bg);
}

/* Log viewer */
.log-viewer-wrapper {
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
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

/* Log row */
.log-row {
  display: flex;
  align-items: center;
  height: var(--flume-row-height);
  padding: 0 8px;
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
  color: var(--flume-fg);
  white-space: nowrap;
  cursor: pointer;
  gap: 8px;
  user-select: none;
  transition: background-color 0.1s;
}

.log-row:nth-child(even) {
  background-color: var(--flume-bg-secondary);
}

.log-row:hover {
  background-color: var(--flume-bg-hover);
}

.log-row--selected {
  background-color: var(--flume-bg-active);
}

.log-row--selected:nth-child(even) {
  background-color: var(--flume-bg-active);
}

.log-row__timestamp {
  flex-shrink: 0;
  color: var(--flume-fg-secondary);
  min-width: 95px;
}

.log-row__level {
  flex-shrink: 0;
  min-width: 48px;
  text-align: center;
  font-weight: 600;
  font-size: 11px;
  border-radius: 3px;
  padding: 1px 4px;
}

.log-row__level--debug {
  color: var(--flume-level-debug);
  background: rgba(107, 114, 128, 0.15);
}

.log-row__level--info {
  color: var(--flume-level-info);
  background: rgba(37, 99, 235, 0.14);
}

.log-row__level--warn {
  color: var(--flume-level-warn);
  background: rgba(217, 119, 6, 0.14);
}

.log-row__level--error {
  color: var(--flume-level-error);
  background: rgba(220, 38, 38, 0.16);
}

.log-row__level--fatal {
  color: var(--flume-level-fatal);
  background: rgba(124, 45, 18, 0.16);
}

.log-row__source {
  flex-shrink: 0;
  color: var(--flume-fg-secondary);
  opacity: 0.6;
  font-size: 11px;
  min-width: 40px;
}

.log-row__content {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.log-row__content--json {
  color: var(--flume-fg-secondary);
}

/* Status bar */
.status-bar {
  display: flex;
  align-items: center;
  height: 28px;
  padding: 0 8px;
  background-color: var(--flume-bg-secondary);
  border-top: 1px solid var(--flume-border);
  font-family: var(--flume-font-family);
  font-size: 11px;
  color: var(--flume-fg-secondary);
  flex-shrink: 0;
  gap: 0;
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
  padding: 2px 8px;
  border-radius: 3px;
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

/* Transition */
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
