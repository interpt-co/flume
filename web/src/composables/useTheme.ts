import { watch, onMounted, computed } from 'vue'
import { useSettingsStore } from '../stores/settings'

export function useTheme() {
  const settingsStore = useSettingsStore()

  const effectiveTheme = computed(() => {
    if (settingsStore.palette === 'catppuccin') {
      return settingsStore.theme === 'dark' ? 'catppuccin-mocha' : 'catppuccin-latte'
    }
    return settingsStore.theme
  })

  function applyTheme(key: string) {
    document.documentElement.setAttribute('data-theme', key)
  }

  onMounted(() => {
    applyTheme(effectiveTheme.value)
  })

  watch(effectiveTheme, (key) => {
    applyTheme(key)
  })

  return {
    theme: settingsStore.theme,
    toggleTheme: settingsStore.toggleTheme,
    palette: settingsStore.palette,
    togglePalette: settingsStore.togglePalette,
  }
}
