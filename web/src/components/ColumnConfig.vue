<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useSettingsStore } from '../stores/settings'

const settings = useSettingsStore()
const root = ref<HTMLElement | null>(null)
const open = ref(false)

function onClickOutside(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) {
    open.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', onClickOutside, true)
})

onUnmounted(() => {
  document.removeEventListener('click', onClickOutside, true)
})
</script>

<template>
  <div class="column-config" ref="root">
    <button class="column-config__trigger" @click="open = !open" title="Configure columns">
      &#x2630;
    </button>
    <div v-if="open" class="column-config__dropdown">
      <label class="column-config__option">
        <input type="checkbox" v-model="settings.showTimestamp"> Timestamp
      </label>
      <label class="column-config__option">
        <input type="checkbox" v-model="settings.showLevel"> Level
      </label>
      <label class="column-config__option">
        <input type="checkbox" v-model="settings.showSource"> Source
      </label>
    </div>
  </div>
</template>

<style scoped>
.column-config {
  position: relative;
}

.column-config__trigger {
  background: none;
  border: 1px solid var(--flume-border);
  border-radius: 4px;
  cursor: pointer;
  font-size: 16px;
  padding: 2px 8px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--flume-fg);
  transition: border-color 0.15s;
}

.column-config__trigger:hover {
  border-color: var(--flume-accent);
}

.column-config__dropdown {
  position: absolute;
  top: 100%;
  right: 0;
  margin-top: 4px;
  background-color: var(--flume-bg-secondary);
  border: 1px solid var(--flume-border);
  border-radius: 4px;
  padding: 6px 0;
  min-width: 140px;
  z-index: 100;
  box-shadow: 0 4px 12px var(--flume-shadow);
}

.column-config__option {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px;
  font-family: var(--flume-font-family);
  font-size: 12px;
  color: var(--flume-fg);
  cursor: pointer;
  white-space: nowrap;
}

.column-config__option:hover {
  background-color: var(--flume-bg-hover);
}

.column-config__option input[type="checkbox"] {
  accent-color: var(--flume-accent);
}
</style>
