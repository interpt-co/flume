import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { LogMessage } from '../types'
import { useLabelsStore } from './labels'

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
    const labelsStore = useLabelsStore()
    const activeLabels = labelsStore.activeLabels
    const hasLabelFilter = Object.keys(activeLabels).length > 0
    const hasTextFilter = !!filter.value

    if (!hasTextFilter && !hasLabelFilter) return messages.value

    let result = messages.value

    // Apply label/level filter client-side for already-displayed messages.
    if (hasLabelFilter) {
      result = result.filter((msg) => {
        for (const [key, val] of Object.entries(activeLabels)) {
          if (key === 'level') {
            if (msg.level?.toLowerCase() !== val.toLowerCase()) return false
          } else {
            if (!msg.labels || msg.labels[key] !== val) return false
          }
        }
        return true
      })
    }

    // Apply text/regex filter.
    if (hasTextFilter) {
      if (filterMode.value === 'regex') {
        try {
          const re = new RegExp(filter.value, 'i')
          result = result.filter((msg) => re.test(msg.content))
        } catch {
          // invalid regex, skip text filter
        }
      } else {
        const term = filter.value.toLowerCase()
        result = result.filter((msg) =>
          msg.content.toLowerCase().includes(term),
        )
      }
    }

    return result
  })

  const seenIds = new Set<string>()

  function rebuildSeenIds() {
    seenIds.clear()
    for (const m of messages.value) {
      seenIds.add(m.id)
    }
  }

  function addMessages(msgs: LogMessage[]) {
    const newMsgs = msgs.filter(m => !seenIds.has(m.id))
    if (newMsgs.length === 0) return

    for (const m of newMsgs) {
      seenIds.add(m.id)
    }
    messages.value.push(...newMsgs)

    if (messages.value.length > MAX_MESSAGES) {
      const excess = messages.value.length - MAX_MESSAGES
      const removed = messages.value.splice(0, excess)
      for (const m of removed) {
        seenIds.delete(m.id)
      }
      oldestLoadedIndex.value += excess
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

  function prependHistory(msgs: LogMessage[], newOldestIndex: number) {
    messages.value = [...msgs, ...messages.value]
    oldestLoadedIndex.value = newOldestIndex
  }

  function setInitialMessages(msgs: LogMessage[], startIndex: number) {
    const historyIds = new Set(msgs.map(m => m.id))
    const wsOnly = messages.value.filter(m => !historyIds.has(m.id))
    messages.value = [...msgs, ...wsOnly]
    oldestLoadedIndex.value = startIndex
    rebuildSeenIds()
  }

  function clear() {
    messages.value = []
    seenIds.clear()
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
