import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { LogMessage } from '../types'

export const WINDOW_SIZE = 500
const MAX_MESSAGES = 5000

export const useLogsStore = defineStore('logs', () => {
  const messages = ref<LogMessage[]>([])
  const filter = ref('')
  const filterMode = ref<'text' | 'regex'>('text')
  const oldestLoadedIndex = ref(0)
  const isLoadingHistory = ref(false)
  const s3HasMore = ref(true)

  const oldestTimestamp = computed(() => {
    if (messages.value.length === 0) return null
    return messages.value[0].ts
  })

  const canLoadMore = computed(() =>
    oldestLoadedIndex.value > 0 || s3HasMore.value
  )

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
    return messages.value.filter((msg) =>
      msg.content.toLowerCase().includes(term),
    )
  })

  function addMessages(msgs: LogMessage[]) {
    const tail = messages.value.slice(-200)
    const existingIds = new Set(tail.map(m => m.id))
    const newMsgs = msgs.filter(m => !existingIds.has(m.id))
    if (newMsgs.length === 0) return

    messages.value.push(...newMsgs)

    if (messages.value.length > MAX_MESSAGES) {
      const excess = messages.value.length - MAX_MESSAGES
      messages.value = messages.value.slice(excess)
      oldestLoadedIndex.value += excess
    }
  }

  function trimToWindow() {
    if (messages.value.length > WINDOW_SIZE) {
      const excess = messages.value.length - WINDOW_SIZE
      messages.value = messages.value.slice(excess)
      oldestLoadedIndex.value += excess
    }
  }

  function prependHistory(msgs: LogMessage[], newOldestIndex: number) {
    messages.value = [...msgs, ...messages.value]
    oldestLoadedIndex.value = newOldestIndex
  }

  function setInitialMessages(msgs: LogMessage[], startIndex: number) {
    const historyIds = new Set(msgs.map(m => m.id))
    const wsOnly = messages.value.filter(m => !historyIds.has(m.id))
    messages.value = [...msgs, ...wsOnly]
    oldestLoadedIndex.value = startIndex
  }

  function clear() {
    messages.value = []
    oldestLoadedIndex.value = 0
    s3HasMore.value = true
  }

  function setFilter(text: string) {
    filter.value = text
  }

  function setFilterMode(mode: 'text' | 'regex') {
    filterMode.value = mode
  }

  return {
    messages,
    filter,
    filterMode,
    filteredMessages,
    oldestLoadedIndex,
    isLoadingHistory,
    s3HasMore,
    oldestTimestamp,
    canLoadMore,
    addMessages,
    trimToWindow,
    prependHistory,
    setInitialMessages,
    clear,
    setFilter,
    setFilterMode,
  }
})
