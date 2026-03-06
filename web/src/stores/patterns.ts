import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { PatternInfo } from '../types'

export const usePatternsStore = defineStore('patterns', () => {
  const available = ref<string[]>([])
  const current = ref<string | null>(null)
  const details = ref<PatternInfo[]>([])

  let pollInterval: ReturnType<typeof setInterval> | null = null
  let selectPatternFn: ((name: string) => void) | null = null

  function setAvailable(patterns: string[], defaultPattern?: string) {
    available.value = patterns
    if (defaultPattern && !current.value) {
      current.value = defaultPattern
    } else if (!current.value && patterns.length > 0) {
      current.value = patterns[0]
    }
  }

  function setCurrent(name: string) {
    current.value = name
    selectPatternFn?.(name)
  }

  function registerSelectPattern(fn: (name: string) => void) {
    selectPatternFn = fn
  }

  async function fetchPatterns() {
    try {
      const res = await fetch('/api/patterns')
      if (res.ok) {
        details.value = await res.json()
        const names = details.value.map(p => p.name)
        if (names.length > 0 && available.value.length === 0) {
          available.value = names
        }
      }
    } catch {
      // ignore
    }
  }

  function startPolling() {
    fetchPatterns()
    if (pollInterval) clearInterval(pollInterval)
    pollInterval = setInterval(fetchPatterns, 15000)
  }

  function stopPolling() {
    if (pollInterval) {
      clearInterval(pollInterval)
      pollInterval = null
    }
  }

  return {
    available,
    current,
    details,
    setAvailable,
    setCurrent,
    registerSelectPattern,
    fetchPatterns,
    startPolling,
    stopPolling,
  }
})
