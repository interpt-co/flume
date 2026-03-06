import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const usePrefilterStore = defineStore('prefilter', () => {
  const filters = ref<Record<string, string>>({})

  const filterParam = computed(() => {
    const entries = Object.entries(filters.value)
    if (entries.length === 0) return ''
    return entries.map(([k, v]) => `${k}:${v}`).join(',')
  })

  const queryString = computed(() => {
    if (!filterParam.value) return ''
    return `filter=${encodeURIComponent(filterParam.value)}`
  })

  function loadFromURL() {
    const params = new URLSearchParams(window.location.search)
    const raw = params.get('filter')
    if (!raw) return

    const parsed: Record<string, string> = {}
    for (const pair of raw.split(',')) {
      const [key, ...rest] = pair.split(':')
      const val = rest.join(':')
      if (key && val) {
        parsed[key.trim()] = val.trim()
      }
    }
    filters.value = parsed
  }

  function setFromServer(preFilters: Record<string, string>) {
    // Only apply server-sent filters if no URL-based filters were loaded.
    if (preFilters && Object.keys(preFilters).length > 0 && Object.keys(filters.value).length === 0) {
      filters.value = preFilters
    }
  }

  return {
    filters,
    filterParam,
    queryString,
    loadFromURL,
    setFromServer,
  }
})
