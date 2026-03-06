<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useLogsStore } from '../stores/logs'

const logsStore = useLogsStore()
const inputEl = ref<HTMLInputElement | null>(null)
const searchText = ref('')
const inputFocused = ref(false)

const regexMode = computed(() => logsStore.filterMode === 'regex')

const filterActive = computed(() => !!logsStore.filter)
const matchCount = computed(() => logsStore.filteredMessages.length)
const totalCount = computed(() => logsStore.messages.length)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

watch(searchText, (val) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    logsStore.setFilter(val)
  }, 200)
})

watch(() => logsStore.filter, (val) => {
  if (val !== searchText.value) {
    searchText.value = val
  }
})

function toggleRegex() {
  logsStore.setFilterMode(regexMode.value ? 'text' : 'regex')
}

function clearSearch() {
  searchText.value = ''
  logsStore.setFilter('')
}

function onGlobalKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
    e.preventDefault()
    inputEl.value?.focus()
  }
}

onMounted(() => {
  document.addEventListener('keydown', onGlobalKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onGlobalKeydown)
  if (debounceTimer) clearTimeout(debounceTimer)
})
</script>

<template>
  <div class="search-bar" :class="{ 'search-bar--focused': inputFocused }">
    <div class="search-bar__icon">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
    </div>
    <input
      ref="inputEl"
      v-model="searchText"
      class="search-bar__input"
      placeholder="Filter logs... (Ctrl+F)"
      @keydown.escape="clearSearch"
      @focus="inputFocused = true"
      @blur="inputFocused = false"
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
</template>

<style scoped>
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
  border-radius: 8px;
  margin: 6px 6px 0;
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
</style>
