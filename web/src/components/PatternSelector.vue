<script setup lang="ts">
import { usePatternsStore } from '../stores/patterns'
const patternsStore = usePatternsStore()
</script>

<template>
  <div v-if="patternsStore.available.length > 0" class="pattern-selector">
    <select
      :value="patternsStore.current"
      @change="patternsStore.setCurrent(($event.target as HTMLSelectElement).value)"
      class="pattern-selector__select"
    >
      <option v-for="p in patternsStore.available" :key="p" :value="p">
        {{ p }}
      </option>
    </select>
  </div>
</template>

<style scoped>
.pattern-selector {
  display: flex;
  align-items: center;
}

.pattern-selector__select {
  background: var(--flume-bg);
  color: var(--flume-fg);
  border: 1px solid var(--flume-border);
  border-radius: 3px;
  padding: 2px 6px;
  font-family: var(--flume-font-family);
  font-size: 12px;
  cursor: pointer;
  outline: none;
}

.pattern-selector__select:hover {
  background-color: var(--flume-bg-hover);
}

.pattern-selector__select:focus {
  border-color: var(--flume-accent);
}
</style>
