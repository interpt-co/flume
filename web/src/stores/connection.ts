import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { StatusData, LogMessage } from '../types'
import { useLogsStore, WINDOW_SIZE } from './logs'

export const useConnectionStore = defineStore('connection', () => {
  const status = ref<'connecting' | 'connected' | 'disconnected'>('disconnected')
  const clientId = ref<string | null>(null)
  const bufferSize = ref(0)
  const serverStats = ref<StatusData | null>(null)
  const following = ref(true)

  const pauseFn = ref<(() => void) | null>(null)
  const resumeFn = ref<(() => void) | null>(null)

  function setConnected(id: string, buffer: number) {
    status.value = 'connected'
    clientId.value = id
    bufferSize.value = buffer
  }

  function setDisconnected() {
    status.value = 'disconnected'
    clientId.value = null
  }

  function updateStats(stats: StatusData) {
    serverStats.value = stats
  }

  function setFollowing(val: boolean) {
    following.value = val
  }

  function registerControls(pause: () => void, resume: () => void) {
    pauseFn.value = pause
    resumeFn.value = resume
  }

  function toggleFollowing() {
    if (following.value) {
      pauseFn.value?.()
    } else {
      resumeFn.value?.()
    }
  }

  async function loadInitialHistory() {
    const logsStore = useLogsStore()
    try {
      const statusRes = await fetch('/api/status')
      const statusData = await statusRes.json()
      const bufUsed = statusData.buffer_used as number
      if (bufUsed === 0) return

      const count = Math.min(bufUsed, WINDOW_SIZE)
      const start = bufUsed - count

      const res = await fetch(`/api/client/load?start=${start}&count=${count}`)
      const data = await res.json() as { messages: LogMessage[]; total: number }
      logsStore.setInitialMessages(data.messages, start)
    } catch {
      // WS stream will provide messages as fallback
    }
  }

  async function loadMoreHistory() {
    const logsStore = useLogsStore()
    if (logsStore.isLoadingHistory || !logsStore.canLoadMore) return

    logsStore.isLoadingHistory = true
    try {
      if (logsStore.oldestLoadedIndex > 0) {
        // Load from ring buffer
        const start = Math.max(0, logsStore.oldestLoadedIndex - WINDOW_SIZE)
        const count = logsStore.oldestLoadedIndex - start
        if (count <= 0) return

        const res = await fetch(`/api/client/load?start=${start}&count=${count}`)
        const data = await res.json() as { messages: LogMessage[]; total: number }
        logsStore.prependHistory(data.messages, start)
      } else if (logsStore.s3HasMore) {
        // Ring buffer exhausted — fall back to S3 history
        const before = logsStore.oldestTimestamp
        if (!before) return

        const res = await fetch(`/api/history?before=${encodeURIComponent(before)}&count=${WINDOW_SIZE}`)
        const data = await res.json() as { messages: LogMessage[]; has_more: boolean }

        if (!data.has_more) {
          logsStore.s3HasMore = false
        }

        if (data.messages && data.messages.length > 0) {
          // API returns newest-first; reverse so oldest is first for prepending
          const sorted = [...data.messages].reverse()
          logsStore.prependHistory(sorted, 0)
        } else {
          logsStore.s3HasMore = false
        }
      }
    } catch {
      // ignore
    } finally {
      logsStore.isLoadingHistory = false
    }
  }

  return {
    status,
    clientId,
    bufferSize,
    serverStats,
    following,
    setConnected,
    setDisconnected,
    updateStats,
    setFollowing,
    registerControls,
    toggleFollowing,
    loadInitialHistory,
    loadMoreHistory,
  }
})
