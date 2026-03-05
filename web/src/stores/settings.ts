import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const STORAGE_KEY = 'flume-settings'

interface SettingsState {
  theme: 'light' | 'dark'
  palette: 'default' | 'catppuccin'
  autoFollow: boolean
  showTimestamp: boolean
  showLevel: boolean
  showSource: boolean
}

function loadFromStorage(): Partial<SettingsState> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return JSON.parse(raw) as Partial<SettingsState>
  } catch {
    // ignore
  }
  return {}
}

function saveToStorage(state: SettingsState) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // ignore
  }
}

export const useSettingsStore = defineStore('settings', () => {
  const saved = loadFromStorage()

  const theme = ref<'light' | 'dark'>(saved.theme ?? 'dark')
  const palette = ref<'default' | 'catppuccin'>(saved.palette ?? 'default')
  const autoFollow = ref(saved.autoFollow ?? true)
  const showTimestamp = ref(saved.showTimestamp ?? true)
  const showLevel = ref(saved.showLevel ?? true)
  const showSource = ref(saved.showSource ?? true)

  function persist() {
    saveToStorage({
      theme: theme.value,
      palette: palette.value,
      autoFollow: autoFollow.value,
      showTimestamp: showTimestamp.value,
      showLevel: showLevel.value,
      showSource: showSource.value,
    })
  }

  watch([theme, palette, autoFollow, showTimestamp, showLevel, showSource], persist)

  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
  }

  function togglePalette() {
    palette.value = palette.value === 'default' ? 'catppuccin' : 'default'
  }

  function setAutoFollow(val: boolean) {
    autoFollow.value = val
  }

  function setShowTimestamp(val: boolean) {
    showTimestamp.value = val
  }

  function setShowLevel(val: boolean) {
    showLevel.value = val
  }

  function setShowSource(val: boolean) {
    showSource.value = val
  }

  return {
    theme,
    palette,
    autoFollow,
    showTimestamp,
    showLevel,
    showSource,
    toggleTheme,
    togglePalette,
    setAutoFollow,
    setShowTimestamp,
    setShowLevel,
    setShowSource,
  }
})
