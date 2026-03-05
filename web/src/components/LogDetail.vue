<script setup lang="ts">
import { computed, ref } from 'vue'
import type { LogMessage } from '../types'

const props = defineProps<{
  message: LogMessage | null
  visible: boolean
}>()

const copyLabel = ref('Copy')

async function copyRaw() {
  if (!props.message) return
  try {
    await navigator.clipboard.writeText(props.message.content)
    copyLabel.value = 'Copied!'
    setTimeout(() => { copyLabel.value = 'Copy' }, 1500)
  } catch {
    // clipboard API may not be available
  }
}

defineEmits<{
  close: []
}>()

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

const highlightedJson = computed(() => {
  if (!props.message?.is_json || !props.message.json_content) return ''
  try {
    const raw = JSON.stringify(props.message.json_content, null, 2)
    const escaped = escapeHtml(raw)
    return escaped.replace(
      /(&quot;(?:[^&]|&(?!quot;))*&quot;)(\s*:)?|(\b(?:true|false|null)\b)|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
      (_match: string, str?: string, colon?: string, bool?: string, num?: string) => {
        if (str) {
          if (colon) {
            return `<span class="json-key">${str}</span>${colon}`
          }
          return `<span class="json-string">${str}</span>`
        }
        if (bool) {
          return `<span class="json-bool">${bool}</span>`
        }
        if (num) {
          return `<span class="json-number">${num}</span>`
        }
        return _match
      },
    )
  } catch {
    return ''
  }
})
</script>

<template>
  <Transition name="slide-up">
    <div v-if="visible && message" class="log-detail">
      <div class="log-detail__header">
        <span class="log-detail__title">Log Detail</span>
        <button class="log-detail__close" @click="$emit('close')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
      <div class="log-detail__body">
        <div class="log-detail__section">
          <h3>Metadata</h3>
          <table class="log-detail__meta">
            <tr><td>Timestamp</td><td>{{ message.ts }}</td></tr>
            <tr><td>Source</td><td>{{ message.source }}</td></tr>
            <tr><td>Origin</td><td>{{ message.origin.name }}</td></tr>
            <tr v-if="message.level"><td>Level</td><td>{{ message.level }}</td></tr>
            <tr v-for="(v, k) in message.labels" :key="k"><td>{{ k }}</td><td>{{ v }}</td></tr>
          </table>
        </div>
        <div v-if="message.is_json" class="log-detail__section">
          <h3>JSON Content</h3>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <pre class="log-detail__json" v-html="highlightedJson"></pre>
        </div>
        <div class="log-detail__section">
          <div class="log-detail__section-header">
            <h3>Raw</h3>
            <button class="log-detail__copy" @click="copyRaw">{{ copyLabel }}</button>
          </div>
          <pre class="log-detail__raw">{{ message.content }}</pre>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
/* Slide-up transition */
.slide-up-enter-active,
.slide-up-leave-active {
  transition: transform 0.2s ease, opacity 0.2s ease;
}
.slide-up-enter-from,
.slide-up-leave-to {
  transform: translateY(20px);
  opacity: 0;
}

.log-detail {
  flex-shrink: 0;
  height: 40%;
  min-height: 120px;
  max-height: 50vh;
  display: flex;
  flex-direction: column;
  border-top: 2px solid var(--flume-accent);
  background-color: var(--flume-bg);
  font-family: var(--flume-font-family);
  font-size: var(--flume-font-size);
}

.log-detail__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  height: 32px;
  background-color: var(--flume-bg-secondary);
  border-bottom: 1px solid var(--flume-border);
  flex-shrink: 0;
}

.log-detail__title {
  font-size: 12px;
  font-weight: 600;
  color: var(--flume-fg);
  letter-spacing: 0.3px;
}

.log-detail__close {
  background: none;
  border: 1px solid var(--flume-border);
  color: var(--flume-fg-secondary);
  padding: 2px 6px;
  border-radius: 3px;
  cursor: pointer;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.log-detail__close:hover {
  background-color: var(--flume-bg-hover);
  color: var(--flume-fg);
}

.log-detail__body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 12px;
}

.log-detail__section {
  margin-bottom: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--flume-border);
}

.log-detail__section:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.log-detail__section h3 {
  margin: 0 0 6px 0;
  font-size: 11px;
  font-weight: 600;
  color: var(--flume-fg-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.log-detail__section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.log-detail__section-header h3 {
  margin: 0;
}

.log-detail__copy {
  background: none;
  border: 1px solid var(--flume-border);
  color: var(--flume-fg-secondary);
  font-family: var(--flume-font-family);
  font-size: 10px;
  padding: 1px 8px;
  border-radius: 3px;
  cursor: pointer;
  line-height: 1.4;
}

.log-detail__copy:hover {
  background-color: var(--flume-bg-hover);
  color: var(--flume-fg);
}

.log-detail__meta {
  border-collapse: collapse;
  font-size: 12px;
}

.log-detail__meta td {
  padding: 2px 12px 2px 0;
  vertical-align: top;
}

.log-detail__meta td:first-child {
  color: var(--flume-fg-secondary);
  font-weight: 600;
  white-space: nowrap;
}

.log-detail__meta td:last-child {
  color: var(--flume-fg);
  word-break: break-all;
}

.log-detail__json,
.log-detail__raw {
  margin: 0;
  padding: 8px;
  background-color: var(--flume-bg-secondary);
  border: 1px solid var(--flume-border);
  border-radius: 4px;
  font-family: var(--flume-font-family);
  font-size: 12px;
  color: var(--flume-fg);
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
  line-height: 1.5;
}

.log-detail__json {
  border-left: 3px solid var(--flume-accent);
}

.log-detail__json :deep(.json-key) {
  color: var(--flume-accent);
}

.log-detail__json :deep(.json-string) {
  color: var(--flume-level-warn);
}

.log-detail__json :deep(.json-number) {
  color: var(--flume-level-info);
}

.log-detail__json :deep(.json-bool) {
  color: var(--flume-level-error);
}
</style>
