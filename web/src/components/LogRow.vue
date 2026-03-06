<script setup lang="ts">
import { computed } from 'vue'
import type { LogMessage } from '../types'
import { useSettingsStore } from '../stores/settings'

const props = defineProps<{
  message: LogMessage
  selected?: boolean
}>()

const settings = useSettingsStore()

const formattedTime = computed(() => {
  const d = new Date(props.message.ts)
  if (isNaN(d.getTime())) return props.message.ts
  const h = String(d.getHours()).padStart(2, '0')
  const m = String(d.getMinutes()).padStart(2, '0')
  const s = String(d.getSeconds()).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
})

const levelClass = computed(() => {
  const level = props.message.level?.toLowerCase() ?? ''
  if (level.includes('fatal')) return 'log-row__level--fatal'
  if (level.includes('error') || level.includes('err')) return 'log-row__level--error'
  if (level.includes('warn')) return 'log-row__level--warn'
  if (level.includes('info')) return 'log-row__level--info'
  if (level.includes('debug') || level.includes('dbg') || level.includes('trace')) return 'log-row__level--debug'
  return ''
})
</script>

<template>
  <div class="log-row" :class="{ 'log-row--selected': selected }">
    <span v-if="settings.showTimestamp" class="log-row__timestamp">{{ formattedTime }}</span>
    <span
      v-if="settings.showLevel && message.level"
      class="log-row__level"
      :class="levelClass"
    >{{ message.level.toUpperCase() }}</span>
    <span v-if="settings.showSource" class="log-row__source">{{ message.source }}</span>
    <span class="log-row__content" :class="{ 'log-row__content--json': message.is_json }">{{ message.content }}</span>
  </div>
</template>

<style scoped>
.log-row {
  display: flex;
  align-items: center;
  height: var(--flume-row-height);
  padding: 0 8px;
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
  color: var(--flume-fg);
  white-space: nowrap;
  cursor: pointer;
  gap: 8px;
  user-select: none;
  transition: background-color 0.1s;
}

.log-row:nth-child(even) {
  background-color: var(--flume-bg-secondary);
}

.log-row:hover {
  background-color: var(--flume-bg-hover);
}

.log-row--selected {
  background-color: var(--flume-bg-active);
}

.log-row--selected:nth-child(even) {
  background-color: var(--flume-bg-active);
}

.log-row__timestamp {
  flex-shrink: 0;
  color: var(--flume-fg-secondary);
  min-width: 95px;
}

.log-row__level {
  flex-shrink: 0;
  min-width: 48px;
  text-align: center;
  font-weight: 600;
  font-size: 11px;
  border-radius: 5px;
  padding: 2px 6px;
}

.log-row__level--debug {
  color: var(--flume-level-debug);
  background: rgba(107, 114, 128, 0.15);
}

.log-row__level--info {
  color: var(--flume-level-info);
  background: rgba(37, 99, 235, 0.14);
}

.log-row__level--warn {
  color: var(--flume-level-warn);
  background: rgba(217, 119, 6, 0.14);
}

.log-row__level--error {
  color: var(--flume-level-error);
  background: rgba(220, 38, 38, 0.16);
}

.log-row__level--fatal {
  color: var(--flume-level-fatal);
  background: rgba(124, 45, 18, 0.16);
}

.log-row__source {
  flex-shrink: 0;
  color: var(--flume-fg-secondary);
  opacity: 0.6;
  font-size: 11px;
  min-width: 40px;
}

.log-row__content {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.log-row__content--json {
  color: var(--flume-fg-secondary);
}
</style>
