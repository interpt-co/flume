import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useLabelsStore = defineStore('labels', () => {
  const activeLabels = ref<Record<string, string>>({})
  const availableLabels = ref<Record<string, string[]>>({})

  let refreshInterval: ReturnType<typeof setInterval> | null = null
  let sendFilterFn: ((labels: Record<string, string>) => void) | null = null

  async function fetchLabels() {
    try {
      const res = await fetch('/api/labels')
      if (res.ok) {
        availableLabels.value = await res.json()
      }
    } catch {
      // ignore fetch errors
    }
  }

  function startPolling() {
    fetchLabels()
    if (refreshInterval) clearInterval(refreshInterval)
    refreshInterval = setInterval(fetchLabels, 30000)
  }

  function stopPolling() {
    if (refreshInterval) {
      clearInterval(refreshInterval)
      refreshInterval = null
    }
  }

  function registerSendFilter(fn: (labels: Record<string, string>) => void) {
    sendFilterFn = fn
  }

  function setLabel(key: string, value: string) {
    activeLabels.value = { ...activeLabels.value, [key]: value }
    sendFilterFn?.(activeLabels.value)
  }

  function removeLabel(key: string) {
    const next = { ...activeLabels.value }
    delete next[key]
    activeLabels.value = next
    sendFilterFn?.(activeLabels.value)
  }

  function clearLabels() {
    activeLabels.value = {}
    sendFilterFn?.({})
  }

  return {
    activeLabels,
    availableLabels,
    fetchLabels,
    startPolling,
    stopPolling,
    registerSendFilter,
    setLabel,
    removeLabel,
    clearLabels,
  }
})
