<script setup lang="ts">
import { useSettingsStore } from '../stores/settings'

const settings = useSettingsStore()
</script>

<template>
  <button
    class="palette-toggle"
    :class="{ 'palette-toggle--active': settings.palette === 'catppuccin' }"
    @click="settings.togglePalette()"
    :title="settings.palette === 'catppuccin' ? 'Switch to default palette' : 'Switch to Catppuccin palette'"
  >
    {{ settings.palette === 'catppuccin' ? 'Catppuccin' : 'Default' }}
  </button>
</template>

<style scoped>
.palette-toggle {
  background: none;
  border: 1px solid var(--flume-border);
  border-radius: 4px;
  cursor: pointer;
  padding: 3px 8px;
  font-size: 11px;
  font-family: var(--flume-font-family);
  color: var(--flume-fg-secondary);
  transition: border-color 0.15s, color 0.15s, background-color 0.15s;
  white-space: nowrap;
}

.palette-toggle:hover {
  border-color: var(--flume-accent);
  color: var(--flume-fg);
}

.palette-toggle--active {
  border-color: var(--flume-accent);
  color: var(--flume-accent);
}
</style>
