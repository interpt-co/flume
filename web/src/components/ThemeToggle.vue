<script setup lang="ts">
import { useSettingsStore } from '../stores/settings'

const settings = useSettingsStore()
</script>

<template>
  <button class="theme-toggle" @click="settings.toggleTheme()" :title="settings.theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'">
    <svg v-if="settings.theme === 'dark'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
    <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
  </button>
</template>

<style scoped>
.theme-toggle {
  background: none;
  border: 1px solid var(--flume-border);
  border-radius: 4px;
  cursor: pointer;
  padding: 4px 8px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--flume-fg-secondary);
  transition: border-color 0.15s, color 0.15s;
}

.theme-toggle:hover {
  border-color: var(--flume-accent);
  color: var(--flume-fg);
}
</style>
