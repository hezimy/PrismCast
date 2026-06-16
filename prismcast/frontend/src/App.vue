<template>
  <div class="app-shell">
    <header class="window-titlebar">
      <div class="titlebar-drag">
        <img src="./assets/logo.png" alt="" class="titlebar-logo" />
        <span class="titlebar-text">PrismCast</span>
      </div>
      <div class="titlebar-controls">
        <button type="button" class="titlebar-btn" @click="minimizeWindow" title="最小化">&#8722;</button>
        <button type="button" class="titlebar-btn" @click="toggleMaximizeWindow" title="最大化">&#9633;</button>
        <button type="button" class="titlebar-btn titlebar-btn-close" @click="hideAppWindow" title="隐藏到托盘">&#10005;</button>
      </div>
    </header>
    <div class="prismcast-app">
    <aside class="sidebar">
      <div class="logo">
        <img src="./assets/logo.png" alt="PrismCast" class="logo-img" />
        <span class="logo-text">PrismCast</span>
      </div>
      <nav class="nav-menu">
        <button v-for="item in navItems" :key="item.id" class="nav-item" :class="{ active: currentView === item.id }" @click="currentView = item.id">
          <span class="nav-icon" v-html="item.icon"></span>
          <span class="nav-label">{{ item.label }}</span>
        </button>
      </nav>
      <div class="sidebar-footer">
        <div class="status-row">
          <div class="status-indicator" :class="deviceInfo.status">
            <span class="status-dot"></span>
            <span class="status-text">{{ statusText }}</span>
          </div>
        </div>
        <div class="protocol-icons">
          <span class="proto-icon active" title="DLNA/UPnP"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M5 12.55a11 11 0 0114.08 0M1.42 9a16 16 0 0121.16 0M8.53 16.11a6 6 0 016.95 0"/></svg></span>
          <span class="proto-icon" title="Chromecast"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg></span>
          <span class="proto-icon" title="AirPlay"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M5 17H4a2 2 0 01-2-2V5a2 2 0 012-2h16a2 2 0 012 2v10a2 2 0 01-2 2h-1"/><polygon points="12 15 17 21 7 21 12 15"/></svg></span>
        </div>
        <div class="version">v{{ deviceInfo.version }}</div>
      </div>
    </aside>
    <main class="main-content">
      <header class="top-bar">
        <h1>{{ pageTitle }}</h1>
        <div class="top-actions">
          <button class="btn-icon theme-toggle" @click="toggleTheme" :title="theme === 'dark' ? t('tooltip.themeDark') : t('tooltip.themeLight')">
            <svg v-if="theme === 'dark'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/></svg>
            <svg v-else width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z"/></svg>
          </button>
        </div>
      </header>
      <div class="content-area">
        <DashboardView v-if="currentView === 'dashboard'" :device-info="deviceInfo" :playback="playbackStatus" :settings="settings" @toggle-cast="handleToggleCast" />
        <SettingsView v-else-if="currentView === 'settings'" :settings="settings" :save="saveSettings" />
        <AboutView v-else-if="currentView === 'about'" :device-info="deviceInfo" />
      </div>
    </main>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import DashboardView from './views/DashboardView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'
import { useI18n } from './i18n'
import { WindowMinimise, WindowToggleMaximise, EventsOn } from '../wailsjs/runtime/runtime'

const { t, setLang } = useI18n()

const currentView = ref('dashboard')
const navItems = computed(() => [
  { id: 'dashboard', label: t('nav.dashboard'), icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/></svg>' },
  { id: 'settings', label: t('nav.settings'), icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z"/></svg>' },
  { id: 'about', label: t('nav.about'), icon: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>' }
])

const pageTitle = computed(() => ({
  dashboard: t('pageTitle.dashboard'),
  settings: t('pageTitle.settings'),
  about: t('pageTitle.about')
}[currentView.value] || 'PrismCast'))

const deviceInfo = ref({ name: 'PrismCast', uuid: '', version: '1.0.0', status: 'running', player: 'idle' })
const playbackStatus = ref({ state: 'idle', title: '', artist: '', duration: 0, position: 0, volume: 100, uri: '', isSeekable: false, isMuted: false })
const settings = ref({ device_name: 'PrismCast', auto_start: false, image_viewer_first: true, volume: 100, language: 'zh-CN', theme: 'dark', log_level: 'main' })
const statusText = computed(() => ({
  running: t('status.running'),
  stopped: t('status.stopped')
}[deviceInfo.value.status] || t('status.unknown')))

const theme = computed(() => settings.value.theme || 'dark')

const themePresets = {
  dark: {
    '--bg-primary': '#1e1035',
    '--bg-sidebar': '#261545',
    '--bg-card': 'rgba(38, 21, 69, 0.5)',
    '--text-primary': '#f3f4f6',
    '--text-secondary': '#9ca3af',
    '--accent-primary': '#835be2',
    '--accent-deep': '#5e33c1',
    '--accent-dark': '#3a197f',
    '--border-color': 'rgba(131, 91, 226, 0.1)',
    '--card-bg': 'rgba(38, 21, 69, 0.5)',
    '--card-border': 'rgba(131, 91, 226, 0.1)',
    '--text-bright': '#f3f4f6',
    '--text-muted': '#9ca3af',
    '--text-dim': '#6b7280',
    '--card-hover': 'rgba(131,91,226,0.05)',
    '--input-bg': 'rgba(30,16,53,0.6)',
    '--progress-track': 'rgba(255,255,255,0.08)',
    '--footer-border': 'rgba(255, 255, 255, 0.05)',
    '--footer-bg': 'linear-gradient(135deg, rgba(131, 91, 226, 0.18) 0%, rgba(58, 25, 127, 0.10) 100%)',
    '--footer-shadow': '0 0 12px rgba(131, 91, 226, 0.10)',
    '--titlebar-bg': 'linear-gradient(90deg, rgba(58, 25, 127, 0.80) 0%, rgba(94, 51, 193, 0.72) 45%, rgba(131, 91, 226, 0.64) 100%)',
    '--titlebar-text': '#ede9fe',
    '--titlebar-border': 'rgba(167, 139, 250, 0.22)'
  },
  light: {
    '--bg-primary': '#f5f3fa',
    '--bg-sidebar': '#ede9f5',
    '--bg-card': 'rgba(255, 255, 255, 0.8)',
    '--text-primary': '#1a1a2e',
    '--text-secondary': '#6b7280',
    '--accent-primary': '#835be2',
    '--accent-deep': '#5e33c1',
    '--accent-dark': '#3a197f',
    '--border-color': 'rgba(131, 91, 226, 0.15)',
    '--card-bg': 'rgba(255, 255, 255, 0.9)',
    '--card-border': 'rgba(131, 91, 226, 0.12)',
    '--text-bright': '#1a1a2e',
    '--text-muted': '#6b7280',
    '--text-dim': '#9ca3af',
    '--card-hover': 'rgba(131,91,226,0.06)',
    '--input-bg': 'rgba(255,255,255,0.8)',
    '--progress-track': 'rgba(131,91,226,0.1)',
    '--footer-border': 'rgba(0,0,0,0.06)',
    '--footer-bg': 'linear-gradient(135deg, rgba(131, 91, 226, 0.20) 0%, rgba(94, 51, 193, 0.10) 100%)',
    '--footer-shadow': '0 0 12px rgba(131, 91, 226, 0.08)',
    '--titlebar-bg': 'linear-gradient(90deg, rgba(237, 233, 245, 0.80) 0%, rgba(210, 196, 240, 0.76) 45%, rgba(168, 140, 230, 0.72) 100%)',
    '--titlebar-text': '#4c1d95',
    '--titlebar-border': 'rgba(131, 91, 226, 0.18)'
  }
}

function applyTheme(themeName) {
  const root = document.documentElement
  const preset = themePresets[themeName] || themePresets.dark
  for (const [key, value] of Object.entries(preset)) {
    root.style.setProperty(key, value)
  }
  root.setAttribute('data-theme', themeName)
}

function toggleTheme() {
  const newTheme = theme.value === 'dark' ? 'light' : 'dark'
  settings.value.theme = newTheme
  applyTheme(newTheme)
  saveSettings({ ...settings.value, theme: newTheme })
}

watch(theme, (newTheme) => {
  applyTheme(newTheme)
}, { immediate: true })

watch(() => settings.value.language, (newLang) => {
  if (newLang) setLang(newLang)
}, { immediate: true })

async function refresh() {
  try {
    if (window.go?.main?.App) {
      const info = await window.go.main.App.GetDeviceInfo()
      deviceInfo.value = { ...deviceInfo.value, ...info }
      const pb = await window.go.main.App.GetPlaybackStatus()
      playbackStatus.value = { ...playbackStatus.value, ...pb }
      if (currentView.value !== 'settings') {
        const cfg = await window.go.main.App.GetSettings()
        settings.value = { ...settings.value, ...cfg }
      }
    }
  } catch (e) { console.error('Refresh failed:', e) }
}

async function handleToggleCast() {
  try {
    if (window.go?.main?.App) {
      await window.go.main.App.ToggleCastService()
      await refresh()
    }
  } catch (e) { console.error('Toggle cast failed:', e) }
}

async function saveSettings(newSettings) {
  try {
    if (window.go?.main?.App) {
      await window.go.main.App.SaveSettings(newSettings)
      const cfg = await window.go.main.App.GetSettings()
      settings.value = { ...settings.value, ...cfg }
      applyTheme(cfg.theme || 'dark')
      if (cfg.language) setLang(cfg.language)
    }
  } catch (e) {
    console.error('Save settings failed:', e)
    throw e
  }
}

function minimizeWindow() {
  try { WindowMinimise() } catch (e) { /* dev browser */ }
}

function toggleMaximizeWindow() {
  try { WindowToggleMaximise() } catch (e) { /* dev browser */ }
}

function hideAppWindow() {
  if (window.go?.main?.App?.HideWindow) {
    window.go.main.App.HideWindow()
  }
}

let pollInterval

function startStatusPolling() {
  if (pollInterval) return
  pollInterval = setInterval(refresh, 2000)
}

function stopStatusPolling() {
  if (!pollInterval) return
  clearInterval(pollInterval)
  pollInterval = null
}

onMounted(async () => {
  await refresh()
  EventsOn('window-visibility', (visible) => {
    if (visible) startStatusPolling()
    else stopStatusPolling()
  })
  try {
    if (window.go?.main?.App?.IsWindowVisible) {
      const visible = await window.go.main.App.IsWindowVisible()
      if (visible) startStatusPolling()
    }
  } catch (e) { /* dev browser */ }
})
onUnmounted(() => stopStatusPolling())
</script>

<style>
.content-area::-webkit-scrollbar { display: none; }
.content-area { -ms-overflow-style: none; scrollbar-width: none; }

.app-shell { display: flex; flex-direction: column; width: 100vw; height: 100vh; overflow: hidden; background: var(--bg-primary); color: var(--text-primary); }

.window-titlebar {
  height: 32px;
  flex-shrink: 0;
  display: flex;
  align-items: stretch;
  background: var(--titlebar-bg);
  border-bottom: 1px solid var(--titlebar-border);
  user-select: none;
}
.titlebar-drag {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  --wails-draggable: drag;
}
.titlebar-logo { width: 18px; height: 18px; object-fit: contain; pointer-events: none; }
.titlebar-text { font-size: 12px; font-weight: 600; color: var(--titlebar-text); letter-spacing: 0.02em; }
.titlebar-controls { display: flex; flex-shrink: 0; --wails-draggable: no-drag; }
.titlebar-btn {
  width: 46px;
  border: none;
  background: transparent;
  color: var(--titlebar-text);
  font-size: 12px;
  line-height: 1;
  cursor: pointer;
  transition: background 0.15s ease;
}
.titlebar-btn:hover { background: rgba(131, 91, 226, 0.18); }
.titlebar-btn-close:hover { background: #e81123; color: #fff; }

.prismcast-app { display: flex; flex: 1; min-height: 0; background: var(--bg-primary); color: var(--text-primary); }

.sidebar { width: 150px; background: var(--bg-sidebar); border-right: 1px solid var(--border-color); display: flex; flex-direction: column; padding: 12px 8px; flex-shrink: 0; }
.logo { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; padding: 0 4px; }
.logo-img { width: 32px; height: 32px; object-fit: contain; }
.logo-text { font-size: 16px; font-weight: 700; background: linear-gradient(135deg, #835be2 0%, #5e33c1 50%, #3a197f 100%); -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text; }

.nav-menu { flex: 1; display: flex; flex-direction: column; gap: 2px; }
.nav-item { display: flex; align-items: center; gap: 8px; padding: 9px 10px; border-radius: 8px; border: none; background: transparent; color: var(--text-secondary); font-size: 13px; font-weight: 500; cursor: pointer; transition: all 0.2s ease; }
.nav-item:hover { background: rgba(131, 91, 226, 0.08); }
[data-theme="light"] .nav-item:hover,
[data-theme="light"] .nav-item.active { color: #5e33c1; }
[data-theme="dark"] .nav-item:hover,
[data-theme="dark"] .nav-item.active { color: #c4b5fd; }
.nav-item.active { background: linear-gradient(135deg, rgba(131, 91, 226, 0.2) 0%, rgba(94, 51, 193, 0.15) 100%); box-shadow: 0 0 12px rgba(131, 91, 226, 0.1); }
.nav-icon { display: flex; align-items: center; justify-content: center; color: inherit; }

.sidebar-footer {
  margin-top: auto;
  padding: 10px 6px 8px;
  border-radius: 8px;
  min-height: 78px;
  box-sizing: border-box;
  background: var(--footer-bg);
  box-shadow: var(--footer-shadow);
}
.status-row { margin-bottom: 6px; display: flex; justify-content: center; min-height: 22px; align-items: center; }
.status-indicator { display: flex; align-items: center; gap: 5px; padding: 4px 8px; border-radius: 8px; font-size: 11px; max-width: 100%; }
[data-theme="dark"] .status-indicator.running .status-dot { background: #d8b4fe; box-shadow: 0 0 8px rgba(216,180,254,0.7); animation: pulse-dot 1.5s ease-in-out infinite; }
[data-theme="light"] .status-indicator.running .status-dot { background: #835be2; box-shadow: 0 0 6px rgba(131,91,226,0.3); animation: pulse-dot 1.5s ease-in-out infinite; }
.status-indicator.stopped .status-dot { background: #ef4444; }
.status-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
@keyframes pulse-dot { 0%, 100% { opacity: 1; transform: scale(1); } 50% { opacity: 0.5; transform: scale(0.8); } }

[data-theme="dark"] .status-text { color: #c4b5fd; font-size: 10px; font-weight: 500; white-space: nowrap; line-height: 14px; }
[data-theme="light"] .status-text { color: #7c3aed; font-size: 10px; font-weight: 600; white-space: nowrap; line-height: 14px; }

.protocol-icons { display: flex; gap: 6px; margin-bottom: 6px; justify-content: center; min-height: 20px; align-items: center; }
.proto-icon { width: 20px; height: 20px; border-radius: 8px; background: transparent; display: flex; align-items: center; justify-content: center; transition: all 0.2s; }
[data-theme="dark"] .proto-icon.active { background: rgba(131,91,226,0.12); color: #c4b5fd; }
[data-theme="light"] .proto-icon.active { background: rgba(124,58,237,0.08); color: #7c3aed; }
.proto-icon:not(.active) { color: var(--text-dim); opacity: 0.45; }

[data-theme="dark"] .version { text-align: center; font-size: 9px; color: #a78bfa; line-height: 12px; min-height: 12px; }
[data-theme="light"] .version { text-align: center; font-size: 9px; color: #8b5cf6; line-height: 12px; min-height: 12px; }

.main-content { flex: 1; display: flex; flex-direction: column; overflow: hidden; min-width: 0; }
.top-bar { display: flex; align-items: center; justify-content: space-between; padding: 8px 16px; border-bottom: 1px solid var(--border-color); flex-shrink: 0; background: transparent; }
.top-bar h1 { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.top-actions { display: flex; gap: 5px; }
.btn-icon { width: 28px; height: 28px; border-radius: 7px; border: none; background: rgba(131, 91, 226, 0.1); color: #a78bfa; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.2s ease; }
.btn-icon:hover { background: rgba(131, 91, 226, 0.2); transform: scale(1.05); }
.theme-toggle { color: #fbbf24; }

.content-area { flex: 1; overflow: hidden; padding: 12px 16px; }
</style>
