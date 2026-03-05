import { ref, onUnmounted } from 'vue'
import { useConnectionStore } from '../stores/connection'
import { useLogsStore } from '../stores/logs'
import type { WSMessage, ClientJoinedData, LogBulkData, StatusData } from '../types'

export function useWebSocket(url: string) {
  const isConnected = ref(false)
  let ws: WebSocket | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let reconnectDelay = 1000
  const MAX_RECONNECT_DELAY = 30000

  const connectionStore = useConnectionStore()
  const logsStore = useLogsStore()

  function connect() {
    if (ws) {
      ws.close()
    }

    connectionStore.status = 'connecting'
    ws = new WebSocket(url)

    ws.onopen = () => {
      isConnected.value = true
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

  connectionStore.registerControls(pause, resume)

  onUnmounted(() => {
    disconnect()
  })

  return {
    connect,
    disconnect,
    send,
    pause,
    resume,
    isConnected,
  }
}
