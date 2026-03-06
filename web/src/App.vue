<script setup lang="ts">
import { ref, onMounted } from 'vue'
import LogViewer from './components/LogViewer.vue'
import LogDetail from './components/LogDetail.vue'
import SearchBar from './components/SearchBar.vue'
import LabelFilter from './components/LabelFilter.vue'
import ColumnConfig from './components/ColumnConfig.vue'
import StatusBar from './components/StatusBar.vue'
import ThemeToggle from './components/ThemeToggle.vue'
import PaletteToggle from './components/PaletteToggle.vue'
import PatternSelector from './components/PatternSelector.vue'
import { useWebSocket } from './composables/useWebSocket'
import { useTheme } from './composables/useTheme'
import { useLabelsStore } from './stores/labels'
import type { LogMessage } from './types'

useTheme()
const labelsStore = useLabelsStore()

const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
const wsUrl = `${wsProtocol}//${window.location.host}/ws`
const { connect } = useWebSocket(wsUrl)

const selectedMessage = ref<LogMessage | null>(null)
const detailVisible = ref(false)

function onRowClick(msg: LogMessage) {
  selectedMessage.value = msg
  detailVisible.value = true
}

function closeDetail() {
  detailVisible.value = false
}

onMounted(() => {
  connect()
  labelsStore.startPolling()
})
</script>

<template>
  <div class="flume-app">
    <header class="flume-header">
      <h1 class="flume-header__title">flume</h1>
      <div class="flume-header__actions">
        <PatternSelector />
        <ColumnConfig />
        <PaletteToggle />
        <ThemeToggle />
      </div>
    </header>
    <SearchBar />
    <LabelFilter />
    <LogViewer class="flume-main" :selected-message="selectedMessage" @row-click="onRowClick" />
    <LogDetail :message="selectedMessage" :visible="detailVisible" @close="closeDetail" />
    <StatusBar />
  </div>
</template>

<style scoped>
.flume-app {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background-color: var(--flume-bg);
  color: var(--flume-fg);
  font-family: var(--flume-font-family);
}

.flume-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  height: 36px;
  border-bottom: 1px solid var(--flume-border);
  background-color: var(--flume-bg-secondary);
  box-shadow: 0 1px 3px var(--flume-shadow);
  flex-shrink: 0;
  z-index: 10;
}

.flume-header__title {
  font-size: 14px;
  font-weight: 600;
  margin: 0;
  letter-spacing: 0.5px;
  color: var(--flume-accent);
}

.flume-header__actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.flume-main {
  flex: 1;
  min-height: 0;
}
</style>
