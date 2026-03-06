# Flume Viewer v0.3.0 — Web Component Feature Parity + UI Refresh

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bring the `<flume-viewer>` web component to full feature parity with the standalone Flume app (history loading, detail panel, label filters) and refresh the UI across both builds.

**Architecture:** The web component (`FlumeViewer.ce.vue`) is a self-contained Vue custom element with no Pinia. All state lives in local refs. A new `baseUrl` prop enables HTTP API calls to Flume's REST endpoints. The same UI refresh (bigger bars, rounded corners, more spacing) is applied to both the WC and the full app's scoped components.

**Tech Stack:** Vue 3.5 custom elements, Vite (WC build), TypeScript

---

## Task 1: Enhance FlumeViewer.ce.vue — Full Rewrite

**Files:**
- Rewrite: `web/src/FlumeViewer.ce.vue`

This is the main task. Rewrite the web component to include all features from the full app.

### New Props

```ts
baseUrl: { default: '', type: String },    // HTTP API prefix (e.g. '/flume')
hideLabels: { default: '', type: String },  // comma-separated label keys to hide
```

### Features to Port

**A. Pattern + Pre-filter tracking (from `useWebSocket.ts` + `prefilter.ts`)**
- Parse `client_joined.default_pattern` → store in `patternName` ref
- Parse `client_joined.pre_filters` → store in `preFilters` ref
- Parse `client_joined.buffer_size` → store in `bufferSize` ref
- Build query params helper: `queryParams()` returns `pattern=X&filter=Y`

**B. Initial history load (from `connection.ts:65-84`)**
- On `client_joined`, call `loadInitialHistory()`:
  1. `GET {baseUrl}/api/status?{queryParams}` → get `buffer_used`
  2. `GET {baseUrl}/api/client/load?start={bufUsed-500}&count=500&{queryParams}` → get messages
  3. Merge with any WS messages already received (dedup by ID, same as `logs.ts:113-119`)

**C. Scroll-up history loading (from `LogViewer.vue:44-61` + `connection.ts:86-127`)**
- In `onScroll()`, detect `scrollTop < 30` → call `loadMoreHistory()`
- Track `oldestLoadedIndex` and `s3HasMore` refs
- Phase 1: if `oldestLoadedIndex > 0` → `GET {baseUrl}/api/client/load?start=X&count=500`
- Phase 2: if ring buffer exhausted and `s3HasMore` → `GET {baseUrl}/api/history?before=TIMESTAMP&count=500`
- Preserve scroll position (measure `scrollHeight` before/after prepend)
- Guard: skip if already loading (`isLoadingHistory`)

**D. Message windowing (from `logs.ts`)**
- Constants: `WINDOW_SIZE = 500`, `MAX_MESSAGES = 5000`
- `addMessages()`: dedup by seenIds, append, cap at MAX_MESSAGES, adjust `oldestLoadedIndex`
- `trimToWindow()`: trim to 500 from tail when resuming auto-follow
- `prependHistory()`: prepend + update `oldestLoadedIndex`
- `setInitialMessages()`: merge initial HTTP load with WS messages (dedup)
- `canLoadMore`: computed from `oldestLoadedIndex > 0 || s3HasMore`
- `oldestTimestamp`: computed from first message's `ts`
- Auto-follow watcher: trim at 600 → 500 (existing, keep)
- Resume auto-follow: scrollToBottom + 10s `trimToWindow` timer (existing, enhance)

**E. Label filter bar (from `LabelFilter.vue` + `labels.ts`)**
- On connect, fetch `GET {baseUrl}/api/labels?{queryParams}`, poll every 30s
- Store `availableLabels: Record<string, string[]>` and `activeLabels: Record<string, string>`
- Compute `sortedKeys`: exclude pre-filter keys + `hideLabels` keys
- Render bar between search bar and log viewer: scope badges + interactive pills + clear button
- On label toggle → send `set_filter` WS message: `{ type: 'set_filter', data: { labels } }`
- Apply active labels to `filteredMessages` computed (both text filter + label filter, same logic as `logs.ts:26-68`)

**F. Log detail panel (from `LogDetail.vue`)**
- Track `selectedMessage` and `detailVisible` refs
- On row click: set both (clicking same row again → close)
- Render slide-up panel: header (title + close button), metadata table (ts, source, origin, level, all labels), JSON highlight section (if `is_json`), raw content with copy button
- Close on: X button click, Escape key
- Escape key: add `keydown` listener on mount, remove on unmount

**G. Reconnect jitter (from `useWebSocket.ts:96`)**
- Add `Math.random() * 0.3 * reconnectDelay` jitter to reconnect timer

### UI Refresh (applied to all bars in the WC styles)

Search bar:
- Height: 32px → 38px
- Add: `border-radius: 8px; margin: 6px 6px 0`
- Input padding: `4px 0` → `6px 4px`
- Gap: `6px` → `8px`
- Padding: `0 8px` → `0 12px`

Label filter bar:
- Padding: `4px 12px` → `6px 12px`
- Pill border-radius: `3px` → `6px`
- Pill padding: `1px 6px` → `2px 8px`
- Scope badge border-radius: `3px` → `8px`
- Scope badge padding: `1px 6px` → `2px 8px`
- Add `border-radius: 8px; margin: 4px 6px 0`

Status bar:
- Height: `28px` → `34px`
- Control border-radius: `3px` → `6px`
- Padding: `0 8px` → `0 12px`
- Add `border-radius: 8px; margin: 0 6px 6px`
- Font-size: `11px` → `12px`

Buttons (.search-bar__btn, .status-bar__control):
- border-radius: `3px` → `6px`
- padding: `1px 8px` → `3px 10px`

Level badges in log rows:
- border-radius: `3px` → `5px`
- padding: `1px 4px` → `2px 6px`

**Step 1:** Rewrite `FlumeViewer.ce.vue` with all features above.
**Step 2:** Build: `cd web && BUILD_WC=true npx vite build`
**Step 3:** Verify the ES module output in `dist-wc/flume-viewer.js`
**Step 4:** Commit: `git add -A && git commit -m "feat(wc): full feature parity — history, labels, detail panel, UI refresh"`

---

## Task 2: Full App UI Refresh + Escape Key

**Files:**
- Modify: `web/src/components/SearchBar.vue` (CSS only)
- Modify: `web/src/components/LabelFilter.vue` (CSS only)
- Modify: `web/src/components/StatusBar.vue` (CSS only)
- Modify: `web/src/components/LogRow.vue` (CSS: level badge radius)
- Modify: `web/src/components/LogDetail.vue` (add Escape key close)

Apply the same UI dimension changes listed in Task 1's UI Refresh section to each component's `<style scoped>` block. These are CSS-only changes except for LogDetail which also needs an Escape key handler.

**LogDetail.vue Escape key:**
```ts
// In setup:
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))
```

**Step 1:** Update SearchBar.vue styles
**Step 2:** Update LabelFilter.vue styles
**Step 3:** Update StatusBar.vue styles
**Step 4:** Update LogRow.vue level badge styles
**Step 5:** Add Escape key handler to LogDetail.vue
**Step 6:** Commit: `git commit -m "feat(ui): refresh bars — bigger, rounded, more spacing"`

---

## Task 3: Version Bump + Tag

**Files:**
- Modify: `web/package.json` (version → 0.3.0)

**Step 1:** Update `web/package.json` version to `"0.3.0"`
**Step 2:** Rebuild WC: `cd web && BUILD_WC=true npx vite build`
**Step 3:** Commit: `git commit -m "chore: bump version to v0.3.0"`
**Step 4:** Tag: `git tag v0.3.0`

---

## Task 4: Update Sudoo Platform

**Files (in sudoo repo):**
- Copy: `flume/web/dist-wc/flume-viewer.js` → `sudoo/platform/frontend/public/flume-viewer.js`
- Modify: `sudoo/platform/frontend/src/components/hosting/LogsPanel.vue` (add base-url, hide-labels props)
- Modify: `sudoo/platform/frontend/index.html` (bump cache-buster `?v=3`)
- Modify: `sudoo/platform/ops.sh` (update image tag to 0.3.0)

**LogsPanel.vue changes:**
```vue
<flume-viewer
  :ws-url="wsUrl"
  base-url="/flume"
  :theme="theme"
  auto-follow="true"
  show-search="true"
  hide-labels="source"
  height="100%"
/>
```

**Step 1:** Copy built `flume-viewer.js` to sudoo frontend
**Step 2:** Update `LogsPanel.vue` with new props
**Step 3:** Update `index.html` cache buster to `?v=3`
**Step 4:** Update `ops.sh` image tag from `0.2.0` to `0.3.0`
**Step 5:** Commit in sudoo: `git commit -m "feat: update flume-viewer.js to v0.3.0 — history, labels, detail panel"`

---

## Task 5: Deploy

**Step 1:** Build and push Flume Docker image: `./platform/ops.sh build-flume`
**Step 2:** Build flume viewer WC: `./platform/ops.sh build-flume-wc`
**Step 3:** Build and push sudoo frontend: `./platform/ops.sh build-frontend`
**Step 4:** Deploy: `./platform/ops.sh deploy`
