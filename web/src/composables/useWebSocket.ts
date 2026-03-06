import { ref, onUnmounted } from 'vue'
import { useConnectionStore } from '../stores/connection'
import { useLogsStore } from '../stores/logs'
import { useLabelsStore } from '../stores/labels'
import { usePatternsStore } from '../stores/patterns'
import { usePrefilterStore } from '../stores/prefilter'
import type { WSMessage, ClientJoinedData, LogBulkData, StatusData, PatternChangedData } from '../types'

export function useWebSocket(url: string | (() => string)) {
  const isConnected = ref(false)
  let ws: WebSocket | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let reconnectDelay = 1000
  const MAX_RECONNECT_DELAY = 30000

  const connectionStore = useConnectionStore()
  const logsStore = useLogsStore()
  const labelsStore = useLabelsStore()
  const patternsStore = usePatternsStore()
  const prefilterStore = usePrefilterStore()

  function connect() {
    if (ws) {
      ws.close()
    }

    connectionStore.status = 'connecting'
    const resolvedUrl = typeof url === 'function' ? url() : url
    ws = new WebSocket(resolvedUrl)

    ws.onopen = () => {
      isConnected.value = true
      connectionStore.status = 'connected'
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
      isConnected.value = false
      connectionStore.setDisconnected()
      scheduleReconnect()
    }

    ws.onerror = () => {
      // onclose will fire after onerror
    }
  }

  function handleMessage(msg: WSMessage) {
    switch (msg.type) {
      case 'client_joined': {
        const data = msg.data as ClientJoinedData
        connectionStore.setConnected(data.client_id, data.buffer_size)
        if (data.patterns) {
          patternsStore.setAvailable(data.patterns, data.default_pattern)
        }
        if (data.pre_filters) {
          prefilterStore.setFromServer(data.pre_filters)
        }
        connectionStore.loadInitialHistory()
        break
      }
      case 'log_bulk': {
        const data = msg.data as LogBulkData
        logsStore.addMessages(data.messages)
        break
      }
      case 'status': {
        const data = msg.data as StatusData
        connectionStore.updateStats(data)
        break
      }
      case 'pattern_changed': {
        const data = msg.data as PatternChangedData
        patternsStore.current = data.pattern
        logsStore.clear()
        labelsStore.fetchLabels()
        connectionStore.loadInitialHistory()
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
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    if (ws) {
      ws.onclose = null // prevent reconnect
      ws.close()
      ws = null
    }
    isConnected.value = false
    connectionStore.setDisconnected()
  }

  function send(msg: unknown) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg))
    }
  }

  function pause() {
    send({ type: 'set_status', data: { status: 'stopped' } })
    connectionStore.setFollowing(false)
  }

  function resume() {
    send({ type: 'set_status', data: { status: 'following' } })
    connectionStore.setFollowing(true)
  }

  function setLabelFilter(labels: Record<string, string>) {
    send({ type: 'set_filter', data: { labels } })
  }

  function selectPattern(name: string) {
    send({ type: 'set_pattern', data: { pattern: name } })
  }

  connectionStore.registerControls(pause, resume)
  labelsStore.registerSendFilter(setLabelFilter)
  patternsStore.registerSelectPattern(selectPattern)

  onUnmounted(() => {
    disconnect()
    connectionStore.registerControls(null, null)
    labelsStore.registerSendFilter(null)
    patternsStore.registerSelectPattern(null)
  })

  return {
    connect,
    disconnect,
    send,
    pause,
    resume,
    setLabelFilter,
    selectPattern,
    isConnected,
  }
}
