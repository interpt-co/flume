<script setup lang="ts">
import { computed } from 'vue'
import { useLabelsStore } from '../stores/labels'
import { usePrefilterStore } from '../stores/prefilter'

const labels = useLabelsStore()
const prefilterStore = usePrefilterStore()

const hasPrefilters = computed(() => Object.keys(prefilterStore.filters).length > 0)
const hasLabels = computed(() => Object.keys(labels.availableLabels).length > 0)
const hasActiveLabels = computed(() => Object.keys(labels.activeLabels).length > 0)
const showBar = computed(() => hasPrefilters.value || hasLabels.value)

// Exclude pre-filtered keys from the interactive label pills.
const sortedKeys = computed(() =>
  Object.keys(labels.availableLabels)
    .filter(k => !(k in prefilterStore.filters))
    .sort()
)

function toggleLabel(key: string, value: string) {
  if (labels.activeLabels[key] === value) {
    labels.removeLabel(key)
  } else {
    labels.setLabel(key, value)
  }
}
</script>

<template>
  <div v-if="showBar" class="label-filter">
    <div class="label-filter__pills">
      <!-- Pre-filter scope badges (non-removable) -->
      <template v-for="(val, key) in prefilterStore.filters" :key="`pre-${key}`">
        <span class="label-filter__scope-badge">{{ key }}:{{ val }}</span>
      </template>

      <!-- Interactive label pills -->
      <template v-for="key in sortedKeys" :key="key">
        <div class="label-filter__group">
          <span class="label-filter__key">{{ key }}:</span>
          <button
            v-for="val in labels.availableLabels[key]"
            :key="`${key}:${val}`"
            class="label-filter__pill"
            :class="{ 'label-filter__pill--active': labels.activeLabels[key] === val }"
            @click="toggleLabel(key, val)"
          >
            {{ val }}
          </button>
        </div>
      </template>
      <button
        v-if="hasActiveLabels"
        class="label-filter__clear"
        @click="labels.clearLabels()"
      >
        clear
      </button>
    </div>
  </div>
</template>

<style scoped>
.label-filter {
  display: flex;
  align-items: center;
  padding: 4px 12px;
  border-bottom: 1px solid var(--flume-border);
  background-color: var(--flume-bg-secondary);
  flex-shrink: 0;
  overflow-x: auto;
}

.label-filter__pills {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.label-filter__group {
  display: flex;
  align-items: center;
  gap: 3px;
}

.label-filter__key {
  font-size: 11px;
  color: var(--flume-fg-secondary);
  font-weight: 600;
  user-select: none;
}

.label-filter__scope-badge {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 3px;
  background-color: var(--flume-accent);
  color: var(--flume-bg);
  font-weight: 600;
  user-select: none;
  opacity: 0.8;
}

.label-filter__pill {
  font-size: 11px;
  padding: 1px 6px;
  border: 1px solid var(--flume-border);
  border-radius: 3px;
  background: var(--flume-bg);
  color: var(--flume-fg);
  cursor: pointer;
  font-family: var(--flume-font-family);
  transition: background-color 0.15s, border-color 0.15s;
}

.label-filter__pill:hover {
  border-color: var(--flume-accent);
}

.label-filter__pill--active {
  background-color: var(--flume-accent);
  color: var(--flume-bg);
  border-color: var(--flume-accent);
}

.label-filter__clear {
  font-size: 10px;
  padding: 1px 5px;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: var(--flume-fg-secondary);
  cursor: pointer;
  font-family: var(--flume-font-family);
  text-decoration: underline;
}

.label-filter__clear:hover {
  color: var(--flume-fg);
}
</style>
