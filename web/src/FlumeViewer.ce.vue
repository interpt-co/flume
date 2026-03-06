<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'

// ---- Types (inlined — no external imports in WC) -------------------------

interface Origin {
  name: string
  meta?: Record<string, string>
}

interface LogMessage {
  id: string
  content: string
  json_content?: unknown
  is_json: boolean
  ts: string
  source: string
  origin: Origin
  labels?: Record<string, string>
  level?: string
}

interface WSMessage {
  type: string
  data?: any
}

interface ClientJoinedData {
  client_id: string
  buffer_size: number
  patterns?: string[]
  default_pattern?: string
  pre_filters?: Record<string, string>
}

interface LogBulkData {
  messages: LogMessage[]
  total: number
}

interface StatusData {
  clients: number
  messages: number
  buffer_used: number
  buffer_capacity: number
}

// ---- Props ---------------------------------------------------------------

const props = withDefaults(
  defineProps<{
    wsUrl?: string
    baseUrl?: string
    theme?: 'light' | 'dark'
    autoFollow?: boolean
    showSearch?: boolean
    hideLabels?: string
    height?: string
  }>(),
  {
    wsUrl: '',
    baseUrl: '',
    theme: 'dark',
    autoFollow: true,
    showSearch: true,
    hideLabels: '',
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

// ---- Constants -----------------------------------------------------------

const WINDOW_SIZE = 500
const MAX_MESSAGES = 5000
const MAX_RECONNECT_DELAY = 30000
const LABEL_POLL_INTERVAL = 30000

// ---- Local state ---------------------------------------------------------

// Connection
const connStatus = ref<'connecting' | 'connected' | 'disconnected'>('disconnected')
const serverStats = ref<StatusData | null>(null)
const patternName = ref<string | null>(null)
const preFilters = ref<Record<string, string>>({})
const bufferSize = ref(0)

// Messages
const messages = ref<LogMessage[]>([])
const seenIds = new Set<string>()
const oldestLoadedIndex = ref(0)
const isLoadingHistory = ref(false)
const s3HasMore = ref(true)

// Filtering
const filter = ref('')
const filterMode = ref<'text' | 'regex'>('text')
const activeLabels = ref<Record<string, string>>({})
const availableLabels = ref<Record<string, string[]>>({})

// UI state
const following = ref(props.autoFollow)
const localAutoFollow = ref(props.autoFollow)
const currentTheme = ref<'light' | 'dark'>(props.theme)
const showTimestamp = ref(true)
const showLevel = ref(true)
const showSource = ref(true)

// ---- Computed: query params helper ----------------------------------------

function queryParams(): string {
  const parts: string[] = []
  if (patternName.value) parts.push(`pattern=${encodeURIComponent(patternName.value)}`)
  const filterEntries = Object.entries(preFilters.value)
  if (filterEntries.length > 0) {
    const filterParam = filterEntries.map(([k, v]) => `${k}:${v}`).join(',')
    parts.push(`filter=${encodeURIComponent(filterParam)}`)
  }
  return parts.join('&')
}

function buildURL(path: string, extraParams?: string): string {
  const base = `${props.baseUrl}${path}`
  const qp = queryParams()
  const allParams = [extraParams, qp].filter(Boolean).join('&')
  return allParams ? `${base}?${allParams}` : base
}

// ---- Computed: filtered messages ------------------------------------------

const filteredMessages = computed(() => {
  const hasLabelFilter = Object.keys(activeLabels.value).length > 0
  const hasTextFilter = !!filter.value

  if (!hasTextFilter && !hasLabelFilter) return messages.value

  let result = messages.value

  // Apply label/level filter
  if (hasLabelFilter) {
    result = result.filter((msg) => {
      for (const [key, val] of Object.entries(activeLabels.value)) {
        if (key === 'level') {
          if (msg.level?.toLowerCase() !== val.toLowerCase()) return false
        } else {
          if (!msg.labels || msg.labels[key] !== val) return false
        }
      }
      return true
    })
  }

  // Apply text/regex filter
  if (hasTextFilter) {
    if (filterMode.value === 'regex') {
      try {
        const re = new RegExp(filter.value, 'i')
        result = result.filter((msg) => re.test(msg.content))
      } catch {
        // invalid regex
      }
    } else {
      const term = filter.value.toLowerCase()
      result = result.filter((msg) => msg.content.toLowerCase().includes(term))
    }
  }

  return result
})

const filterActive = computed(() => !!filter.value)
const matchCount = computed(() => filteredMessages.value.length)
const totalCount = computed(() => messages.value.length)
const regexMode = computed(() => filterMode.value === 'regex')

const canLoadMore = computed(() => oldestLoadedIndex.value > 0 || s3HasMore.value)
const oldestTimestamp = computed(() => {
  if (messages.value.length === 0) return null
  return messages.value[0].ts
})

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

// ---- Computed: label filter bar -------------------------------------------

const hiddenLabelKeys = computed(() => {
  const keys = new Set(Object.keys(preFilters.value))
  if (props.hideLabels) {
    for (const k of props.hideLabels.split(',')) {
      const trimmed = k.trim()
      if (trimmed) keys.add(trimmed)
    }
  }
  return keys
})

const sortedLabelKeys = computed(() =>
  Object.keys(availableLabels.value)
    .filter(k => !hiddenLabelKeys.value.has(k))
    .sort()
)

const hasPreFilters = computed(() => Object.keys(preFilters.value).length > 0)
const hasLabels = computed(() => Object.keys(availableLabels.value).length > 0)
const hasActiveLabels = computed(() => Object.keys(activeLabels.value).length > 0)
const showLabelBar = computed(() => hasPreFilters.value || hasLabels.value)

// ---- Helpers --------------------------------------------------------------

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

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#x27;')
}

// ---- WebSocket -----------------------------------------------------------

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectDelay = 1000
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
      // ignore malformed
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
      bufferSize.value = data.buffer_size || 0
      if (data.default_pattern) {
        patternName.value = data.default_pattern
      }
      if (data.pre_filters && Object.keys(data.pre_filters).length > 0) {
        preFilters.value = data.pre_filters
      }
      emit('flume:connected', data.client_id)
      loadInitialHistory()
      fetchLabels()
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
    case 'pattern_changed': {
      const data = msg.data as { pattern: string; buffer_size: number; buffer_used: number }
      patternName.value = data.pattern
      clearMessages()
      fetchLabels()
      loadInitialHistory()
      break
    }
  }
}

function scheduleReconnect() {
  if (reconnectTimer) return
  const jitter = Math.random() * 0.3 * reconnectDelay
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY)
    connect()
  }, reconnectDelay + jitter)
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

// ---- Message management ---------------------------------------------------

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
    const removed = messages.value.splice(0, excess)
    for (const m of removed) seenIds.delete(m.id)
    oldestLoadedIndex.value += excess
  }
}

function prependHistory(msgs: LogMessage[], newOldestIndex: number) {
  // Dedup against existing
  const newMsgs = msgs.filter((m) => !seenIds.has(m.id))
  for (const m of newMsgs) seenIds.add(m.id)
  messages.value = [...newMsgs, ...messages.value]
  oldestLoadedIndex.value = newOldestIndex
}

function setInitialMessages(msgs: LogMessage[], startIndex: number) {
  const historyIds = new Set(msgs.map(m => m.id))
  const wsOnly = messages.value.filter(m => !historyIds.has(m.id))
  messages.value = [...msgs, ...wsOnly]
  oldestLoadedIndex.value = startIndex
  rebuildSeenIds()
}

function rebuildSeenIds() {
  seenIds.clear()
  for (const m of messages.value) {
    seenIds.add(m.id)
  }
}

function trimToWindow() {
  if (messages.value.length > WINDOW_SIZE) {
    const excess = messages.value.length - WINDOW_SIZE
    const removed = messages.value.slice(0, excess)
    messages.value = messages.value.slice(excess)
    for (const m of removed) seenIds.delete(m.id)
    oldestLoadedIndex.value += excess
  }
}

function clearMessages() {
  messages.value = []
  seenIds.clear()
  oldestLoadedIndex.value = 0
  s3HasMore.value = true
}

// ---- History loading ------------------------------------------------------

async function loadInitialHistory() {
  if (!props.baseUrl) return
  try {
    const statusRes = await fetch(buildURL('/api/status'))
    if (!statusRes.ok) return
    const statusData = await statusRes.json() as StatusData
    const bufUsed = statusData.buffer_used
    if (bufUsed === 0) return

    const count = Math.min(bufUsed, WINDOW_SIZE)
    const start = bufUsed - count

    const res = await fetch(buildURL('/api/client/load', `start=${start}&count=${count}`))
    if (!res.ok) return
    const data = await res.json() as { messages: LogMessage[]; total: number }
    setInitialMessages(data.messages, start)
  } catch {
    // WS stream will provide messages as fallback
  }
}

async function loadMoreHistory() {
  if (isLoadingHistory.value || !canLoadMore.value || !props.baseUrl) return

  isLoadingHistory.value = true
  try {
    if (oldestLoadedIndex.value > 0) {
      // Phase 1: Load from ring buffer
      const start = Math.max(0, oldestLoadedIndex.value - WINDOW_SIZE)
      const count = oldestLoadedIndex.value - start
      if (count <= 0) return

      const res = await fetch(buildURL('/api/client/load', `start=${start}&count=${count}`))
      if (!res.ok) return
      const data = await res.json() as { messages: LogMessage[]; total: number }
      prependHistory(data.messages, start)
    } else if (s3HasMore.value) {
      // Phase 2: Fall back to S3 history
      const before = oldestTimestamp.value
      if (!before) return

      const res = await fetch(buildURL('/api/history', `before=${encodeURIComponent(before)}&count=${WINDOW_SIZE}`))
      if (!res.ok) return
      const data = await res.json() as { messages: LogMessage[]; has_more: boolean }

      if (!data.has_more) {
        s3HasMore.value = false
      }

      if (data.messages && data.messages.length > 0) {
        const sorted = [...data.messages].reverse()
        prependHistory(sorted, 0)
      } else {
        s3HasMore.value = false
      }
    }
  } catch {
    // ignore
  } finally {
    isLoadingHistory.value = false
  }
}

// ---- Labels ---------------------------------------------------------------

let labelPollTimer: ReturnType<typeof setInterval> | null = null

async function fetchLabels() {
  if (!props.baseUrl) return
  try {
    const res = await fetch(buildURL('/api/labels'))
    if (res.ok) {
      availableLabels.value = await res.json()
    }
  } catch {
    // ignore
  }
}

function startLabelPolling() {
  fetchLabels()
  if (labelPollTimer) clearInterval(labelPollTimer)
  labelPollTimer = setInterval(fetchLabels, LABEL_POLL_INTERVAL)
}

function stopLabelPolling() {
  if (labelPollTimer) {
    clearInterval(labelPollTimer)
    labelPollTimer = null
  }
}

function toggleLabel(key: string, value: string) {
  if (activeLabels.value[key] === value) {
    const next = { ...activeLabels.value }
    delete next[key]
    activeLabels.value = next
  } else {
    activeLabels.value = { ...activeLabels.value, [key]: value }
  }
  send({ type: 'set_filter', data: { labels: activeLabels.value } })
}

function clearLabels() {
  activeLabels.value = {}
  send({ type: 'set_filter', data: { labels: {} } })
}

// ---- Scroll / auto-follow ------------------------------------------------

const logContainer = ref<HTMLElement | null>(null)
const showFollowButton = ref(false)
let trimTimer: ReturnType<typeof setTimeout> | null = null

function onScroll() {
  if (!logContainer.value) return
  const { scrollTop, scrollHeight, clientHeight } = logContainer.value
  const atBottom = scrollHeight - scrollTop - clientHeight < 30
  const atTop = scrollTop < 30

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

    if (atTop && canLoadMore.value && !isLoadingHistory.value) {
      loadHistoryWithScroll()
    }
  }
}

async function loadHistoryWithScroll() {
  if (!logContainer.value) return
  const prevScrollHeight = logContainer.value.scrollHeight

  await loadMoreHistory()

  await nextTick()
  if (logContainer.value) {
    const newScrollHeight = logContainer.value.scrollHeight
    logContainer.value.scrollTop += newScrollHeight - prevScrollHeight
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
    if (localAutoFollow.value) {
      trimToWindow()
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
        trimToWindow()
      }
      await nextTick()
      scrollToBottom()
    }
  },
)

// ---- Search bar -----------------------------------------------------------

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

// ---- Detail panel ---------------------------------------------------------

const selectedMessage = ref<LogMessage | null>(null)
const detailVisible = ref(false)
const copyLabel = ref('Copy')

function onRowClick(msg: LogMessage) {
  if (selectedMessage.value?.id === msg.id) {
    detailVisible.value = !detailVisible.value
  } else {
    selectedMessage.value = msg
    detailVisible.value = true
  }
  emit('flume:row-click', msg)
}

function closeDetail() {
  detailVisible.value = false
}

async function copyRaw() {
  if (!selectedMessage.value) return
  try {
    await navigator.clipboard.writeText(selectedMessage.value.content)
    copyLabel.value = 'Copied!'
    setTimeout(() => { copyLabel.value = 'Copy' }, 1500)
  } catch {
    // clipboard API may not be available
  }
}

const highlightedJson = computed(() => {
  if (!selectedMessage.value?.is_json || !selectedMessage.value.json_content) return ''
  try {
    const raw = JSON.stringify(selectedMessage.value.json_content, null, 2)
    const escaped = escapeHtml(raw)
    return escaped.replace(
      /(&quot;(?:[^&]|&(?!quot;))*&quot;)(\s*:)?|(\b(?:true|false|null)\b)|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
      (_match: string, str?: string, colon?: string, bool?: string, num?: string) => {
        if (str) {
          if (colon) return `<span class="json-key">${str}</span>${colon}`
          return `<span class="json-string">${str}</span>`
        }
        if (bool) return `<span class="json-bool">${bool}</span>`
        if (num) return `<span class="json-number">${num}</span>`
        return _match
      },
    )
  } catch {
    return ''
  }
})

// ---- Keyboard shortcuts ---------------------------------------------------

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && detailVisible.value) {
    closeDetail()
  }
}

// ---- Prop watchers --------------------------------------------------------

watch(() => props.theme, (val) => {
  currentTheme.value = val
})

watch(() => props.autoFollow, (val) => {
  following.value = val
  localAutoFollow.value = val
})

// ---- Lifecycle ------------------------------------------------------------

onMounted(() => {
  if (props.wsUrl) {
    connect()
  }
  if (localAutoFollow.value) {
    nextTick(() => scrollToBottom())
  }
  if (props.baseUrl) {
    startLabelPolling()
  }
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  disconnect()
  stopLabelPolling()
  cancelTrimTimer()
  if (debounceTimer) clearTimeout(debounceTimer)
  document.removeEventListener('keydown', onKeydown)
})

// ---- Public API -----------------------------------------------------------

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

    <!-- Label filter bar -->
    <div v-if="showLabelBar" class="label-filter">
      <div class="label-filter__pills">
        <!-- Pre-filter scope badges (non-removable) -->
        <template v-for="(val, key) in preFilters" :key="`pre-${key}`">
          <span class="label-filter__scope-badge">{{ key }}:{{ val }}</span>
        </template>

        <!-- Interactive label pills -->
        <template v-for="key in sortedLabelKeys" :key="key">
          <div class="label-filter__group">
            <span class="label-filter__key">{{ key }}:</span>
            <button
              v-for="val in availableLabels[key]"
              :key="`${key}:${val}`"
              class="label-filter__pill"
              :class="{ 'label-filter__pill--active': activeLabels[key] === val }"
              @click="toggleLabel(key, val)"
            >
              {{ val }}
            </button>
          </div>
        </template>
        <button
          v-if="hasActiveLabels"
          class="label-filter__clear"
          @click="clearLabels()"
        >
          clear
        </button>
      </div>
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

    <!-- Detail panel -->
    <Transition name="slide-up">
      <div v-if="detailVisible && selectedMessage" class="log-detail">
        <div class="log-detail__header">
          <span class="log-detail__title">Log Detail</span>
          <button class="log-detail__close" @click="closeDetail">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>
        <div class="log-detail__body">
          <div class="log-detail__section">
            <h3>Metadata</h3>
            <table class="log-detail__meta">
              <tr><td>Timestamp</td><td>{{ selectedMessage.ts }}</td></tr>
              <tr><td>Source</td><td>{{ selectedMessage.source }}</td></tr>
              <tr><td>Origin</td><td>{{ selectedMessage.origin.name }}</td></tr>
              <tr v-if="selectedMessage.level"><td>Level</td><td>{{ selectedMessage.level }}</td></tr>
              <tr v-for="(v, k) in selectedMessage.labels" :key="k"><td>{{ k }}</td><td>{{ v }}</td></tr>
            </table>
          </div>
          <div v-if="selectedMessage.is_json" class="log-detail__section">
            <h3>JSON Content</h3>
            <!-- eslint-disable-next-line vue/no-v-html -->
            <pre class="log-detail__json" v-html="highlightedJson"></pre>
          </div>
          <div class="log-detail__section">
            <div class="log-detail__section-header">
              <h3>Raw</h3>
              <button class="log-detail__copy" @click="copyRaw">{{ copyLabel }}</button>
            </div>
            <pre class="log-detail__raw">{{ selectedMessage.content }}</pre>
          </div>
        </div>
      </div>
    </Transition>

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
/* Reset */
*, *::before, *::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

/* Theme tokens — light (Catppuccin Latte) */
:host,
.ilv-root,
.ilv-root[data-theme="light"] {
  --flume-bg: #eff1f5;
  --flume-bg-secondary: #e6e9ef;
  --flume-fg: #4c4f69;
  --flume-fg-secondary: #6c6f85;
  --flume-accent: #1e66f5;
  --flume-border: #ccd0da;
  --flume-font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
  --flume-font-size: 13px;
  --flume-row-height: 24px;
  --flume-level-debug: #8c8fa1;
  --flume-level-info: #1e66f5;
  --flume-level-warn: #df8e1d;
  --flume-level-error: #d20f39;
  --flume-level-fatal: #fe640b;
  --flume-bg-hover: #dce0e8;
  --flume-bg-active: #bcc0cc;
  --flume-shadow: rgba(0, 0, 0, 0.08);
  --flume-text-accent: #7287fd;
}

/* Theme tokens — dark (Catppuccin Mocha) */
.ilv-root[data-theme="dark"] {
  --flume-bg: #1e1e2e;
  --flume-bg-secondary: #181825;
  --flume-fg: #cdd6f4;
  --flume-fg-secondary: #a6adc8;
  --flume-accent: #89b4fa;
  --flume-border: #313244;
  --flume-level-debug: #9399b2;
  --flume-level-info: #89b4fa;
  --flume-level-warn: #f9e2af;
  --flume-level-error: #f38ba8;
  --flume-level-fatal: #fab387;
  --flume-bg-hover: #313244;
  --flume-bg-active: #45475a;
  --flume-shadow: rgba(0, 0, 0, 0.3);
  --flume-text-accent: #b4befe;
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
  height: 38px;
  padding: 0 12px;
  border-bottom: 1px solid var(--flume-border);
  border-left: 2px solid transparent;
  background-color: var(--flume-bg-secondary);
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
  flex-shrink: 0;
  gap: 8px;
  transition: border-color 0.15s;
  border-radius: 8px;
  margin: 6px 6px 0;
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
  padding: 6px 4px;
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
  padding: 3px 10px;
  border-radius: 6px;
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

/* Label filter bar */
.label-filter {
  display: flex;
  align-items: center;
  padding: 6px 12px;
  border-bottom: 1px solid var(--flume-border);
  background-color: var(--flume-bg-secondary);
  flex-shrink: 0;
  overflow-x: auto;
  border-radius: 8px;
  margin: 4px 6px 0;
}

.label-filter__pills {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.label-filter__group {
  display: flex;
  align-items: center;
  gap: 3px;
}

.label-filter__key {
  font-size: 11px;
  color: var(--flume-fg-secondary);
  font-weight: 600;
  user-select: none;
}

.label-filter__scope-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 8px;
  background-color: var(--flume-accent);
  color: var(--flume-bg);
  font-weight: 600;
  user-select: none;
  opacity: 0.8;
}

.label-filter__pill {
  font-size: 11px;
  padding: 2px 8px;
  border: 1px solid var(--flume-border);
  border-radius: 6px;
  background: var(--flume-bg);
  color: var(--flume-fg);
  cursor: pointer;
  font-family: var(--flume-font-family);
  transition: background-color 0.15s, border-color 0.15s;
}

.label-filter__pill:hover {
  border-color: var(--flume-accent);
}

.label-filter__pill--active {
  background-color: var(--flume-accent);
  color: var(--flume-bg);
  border-color: var(--flume-accent);
}

.label-filter__clear {
  font-size: 10px;
  padding: 1px 5px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--flume-fg-secondary);
  cursor: pointer;
  font-family: var(--flume-font-family);
  text-decoration: underline;
}

.label-filter__clear:hover {
  color: var(--flume-fg);
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
  border-radius: 5px;
  padding: 2px 6px;
}

.log-row__level--debug {
  color: var(--flume-level-debug);
  background: color-mix(in srgb, var(--flume-level-debug) 15%, transparent);
}

.log-row__level--info {
  color: var(--flume-level-info);
  background: color-mix(in srgb, var(--flume-level-info) 14%, transparent);
}

.log-row__level--warn {
  color: var(--flume-level-warn);
  background: color-mix(in srgb, var(--flume-level-warn) 14%, transparent);
}

.log-row__level--error {
  color: var(--flume-level-error);
  background: color-mix(in srgb, var(--flume-level-error) 16%, transparent);
}

.log-row__level--fatal {
  color: var(--flume-level-fatal);
  background: color-mix(in srgb, var(--flume-level-fatal) 16%, transparent);
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

/* Detail panel */
.slide-up-enter-active,
.slide-up-leave-active {
  transition: transform 0.2s ease, opacity 0.2s ease;
}
.slide-up-enter-from,
.slide-up-leave-to {
  transform: translateY(20px);
  opacity: 0;
}

.log-detail {
  flex-shrink: 0;
  height: 40%;
  min-height: 120px;
  max-height: 50vh;
  display: flex;
  flex-direction: column;
  border-top: 2px solid var(--flume-accent);
  background-color: var(--flume-bg);
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
}

.log-detail__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  height: 34px;
  background-color: var(--flume-bg-secondary);
  border-bottom: 1px solid var(--flume-border);
  flex-shrink: 0;
}

.log-detail__title {
  font-size: 12px;
  font-weight: 600;
  color: var(--flume-fg);
  letter-spacing: 0.3px;
}

.log-detail__close {
  background: none;
  border: 1px solid var(--flume-border);
  color: var(--flume-fg-secondary);
  padding: 3px 8px;
  border-radius: 6px;
  cursor: pointer;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.log-detail__close:hover {
  background-color: var(--flume-bg-hover);
  color: var(--flume-fg);
}

.log-detail__body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 12px;
}

.log-detail__section {
  margin-bottom: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--flume-border);
}

.log-detail__section:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.log-detail__section h3 {
  margin: 0 0 6px 0;
  font-size: 11px;
  font-weight: 600;
  color: var(--flume-fg-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.log-detail__section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.log-detail__section-header h3 {
  margin: 0;
}

.log-detail__copy {
  background: none;
  border: 1px solid var(--flume-border);
  color: var(--flume-fg-secondary);
  font-family: var(--flume-font-family);
  font-size: 10px;
  padding: 2px 8px;
  border-radius: 6px;
  cursor: pointer;
  line-height: 1.4;
}

.log-detail__copy:hover {
  background-color: var(--flume-bg-hover);
  color: var(--flume-fg);
}

.log-detail__meta {
  border-collapse: collapse;
  font-size: 12px;
}

.log-detail__meta td {
  padding: 2px 12px 2px 0;
  vertical-align: top;
}

.log-detail__meta td:first-child {
  color: var(--flume-fg-secondary);
  font-weight: 600;
  white-space: nowrap;
}

.log-detail__meta td:last-child {
  color: var(--flume-fg);
  word-break: break-all;
}

.log-detail__json,
.log-detail__raw {
  margin: 0;
  padding: 8px;
  background-color: var(--flume-bg-secondary);
  border: 1px solid var(--flume-border);
  border-radius: 6px;
  font-family: var(--flume-font-family);
  font-size: 12px;
  color: var(--flume-fg);
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
  line-height: 1.5;
}

.log-detail__json {
  border-left: 3px solid var(--flume-accent);
}

.log-detail__json .json-key {
  color: var(--flume-accent);
}

.log-detail__json .json-string {
  color: var(--flume-level-warn);
}

.log-detail__json .json-number {
  color: var(--flume-level-info);
}

.log-detail__json .json-bool {
  color: var(--flume-level-error);
}

/* Status bar */
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
  background-color: #a6e3a1;
}

.status-bar__dot--connecting {
  background-color: #f9e2af;
  animation: pulse-dot 1.5s ease-in-out infinite;
}

@keyframes pulse-dot {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.status-bar__dot--disconnected {
  background-color: #f38ba8;
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
  color: #f9e2af;
}

/* Transitions */
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
