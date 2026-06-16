<template>
  <div class="dashboard">
    <div class="protocols-grid">
      <div class="protocol-card" :class="{ active: isCastRunning, toggling: toggling }" @click="onToggleCast">
        <div class="protocol-icon"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12.55a11 11 0 0114.08 0M1.42 9a16 16 0 0121.16 0M8.53 16.11a6 6 0 016.95 0"/></svg></div>
        <div class="protocol-name">{{ t('dashboard.dlna') }}</div>
        <div class="protocol-status">{{ isCastRunning ? t('dashboard.statusRunning') : t('dashboard.statusStopped') }}</div>
      </div>
      <div class="protocol-card disabled">
        <div class="protocol-icon"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg></div>
        <div class="protocol-name">{{ t('dashboard.chromecast') }}</div>
        <div class="protocol-status">{{ t('dashboard.statusPlanned') }}</div>
      </div>
      <div class="protocol-card disabled">
        <div class="protocol-icon"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 17H4a2 2 0 01-2-2V5a2 2 0 012-2h16a2 2 0 012 2v10a2 2 0 01-2 2h-1"/><polygon points="12 15 17 21 7 21 12 15"/></svg></div>
        <div class="protocol-name">{{ t('dashboard.airplay') }}</div>
        <div class="protocol-status">{{ t('dashboard.statusPlanned') }}</div>
      </div>
    </div>

    <div class="device-name-row">{{ deviceInfo.name || 'PrismCast' }}</div>

    <div v-if="hasActiveCast" class="cast-panel">
      <div class="media-section-title">{{ t('dashboard.currentCast') }}</div>
      <div class="media-info-grid">
        <div class="media-info-item">
          <span class="media-info-label">{{ t('dashboard.labelTitle') }}</span>
          <span class="media-info-value">{{ castMedia.title || '—' }}</span>
        </div>
        <div class="media-info-item">
          <span class="media-info-label">{{ t('dashboard.labelType') }}</span>
          <span class="media-info-value media-type-badge" :data-type="castMedia.mediaType || 'none'">{{ mediaTypeLabel }}</span>
        </div>
        <div class="media-info-item">
          <span class="media-info-label">{{ t('dashboard.labelState') }}</span>
          <span class="media-info-value state-badge" :data-state="castMedia.state || 'idle'">{{ stateLabel }}</span>
        </div>
        <div class="media-info-item uri-row" v-if="castMedia.uri">
          <span class="media-info-label">{{ t('dashboard.labelUri') }}</span>
          <div class="uri-copy-group">
            <span class="media-info-value uri-text" :title="castMedia.uri">{{ displayUri }}</span>
            <button class="copy-btn" @click.stop="copyUri" :title="castMedia.uri">{{ copied ? '✓' : '⧉' }}</button>
          </div>
        </div>
        <div class="media-info-item progress-row" v-if="showProgress">
          <span class="media-info-label">{{ t('dashboard.labelProgress') }}</span>
          <div class="progress-group">
            <div class="progress-bar">
              <div class="progress-fill" :style="{ width: progressPercent + '%' }"></div>
            </div>
            <span class="progress-time">{{ formatTime(playback.position) }} / {{ formatTime(playback.duration) }}</span>
          </div>
        </div>
        <div class="media-info-item" v-if="showProgress">
          <span class="media-info-label">{{ t('dashboard.labelVolume') }}</span>
          <span class="media-info-value">{{ playback.volume ?? '—' }}%</span>
        </div>
      </div>
    </div>

    <div v-else class="hint-panel">
      <img src="../assets/logo.png" alt="PrismCast" class="hint-logo" />
      <div class="hint-text">{{ isCastRunning ? t('dashboard.hintWaiting') : t('dashboard.hintStart') }}</div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from '../i18n'

const { t } = useI18n()

const props = defineProps({
  deviceInfo: { type: Object, default: () => ({}) },
  playback: { type: Object, default: () => ({}) },
  settings: { type: Object, default: () => ({}) }
})

const emit = defineEmits(['toggle-cast'])

const toggling = ref(false)
const copied = ref(false)

async function copyUri() {
  const uri = castMedia.value?.uri || ''
  if (!uri) return
  try {
    await navigator.clipboard.writeText(uri)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    // fallback
    const ta = document.createElement('textarea')
    ta.value = uri
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  }
}

async function onToggleCast() {
  if (toggling.value) return
  toggling.value = true
  try {
    emit('toggle-cast')
    await new Promise(r => setTimeout(r, 1500))
  } finally {
    toggling.value = false
  }
}

const isCastRunning = computed(() => {
  return props.deviceInfo?.castEnabled === true
})

const castMedia = computed(() => {
  return props.deviceInfo?.castMedia || {}
})

const playback = computed(() => props.playback || {})

const hasActiveCast = computed(() => {
  const cm = castMedia.value
  if (!cm.uri) return false
  return cm.state && cm.state !== 'idle'
})

const displayUri = computed(() => {
  const uri = castMedia.value?.uri || ''
  if (!uri) return ''
  try {
    const u = new URL(uri)
    const filename = u.pathname.split('/').pop() || u.pathname
    return filename.length > 40 ? filename.substring(0, 37) + '...' : filename
  } catch {
    return uri.length > 50 ? uri.substring(0, 47) + '...' : uri
  }
})

const mediaTypeLabel = computed(() => {
  const map = {
    video: t('dashboard.mediaTypeVideo'),
    audio: t('dashboard.mediaTypeAudio'),
    image: t('dashboard.mediaTypeImage'),
    document: t('dashboard.mediaTypeDocument')
  }
  return map[castMedia.value?.mediaType] || '—'
})

const stateLabel = computed(() => {
  const map = {
    playing: t('dashboard.statePlaying'),
    paused: t('dashboard.statePaused'),
    loading: t('dashboard.stateLoading'),
    idle: t('dashboard.stateIdle'),
    stopped: t('dashboard.stateStopped'),
    error: t('dashboard.stateError')
  }
  return map[castMedia.value?.state] || t('dashboard.stateIdle')
})

const showProgress = computed(() => {
  const state = castMedia.value?.state
  return (state === 'playing' || state === 'paused') && (playback.value?.duration > 0 || playback.value?.position > 0)
})

const progressPercent = computed(() => {
  const dur = playback.value?.duration || 0
  const pos = playback.value?.position || 0
  if (dur <= 0) return 0
  return Math.min(100, Math.round((pos / dur) * 100))
})

function formatTime(seconds) {
  const s = Math.max(0, Math.floor(seconds || 0))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
  return `${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}
</script>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.protocols-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
.protocol-card {
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  border-radius: 10px;
  padding: 14px 10px;
  text-align: center;
  cursor: pointer;
  transition: all 0.25s ease;
  user-select: none;
}
.protocol-card:hover:not(.disabled) {
  border-color: rgba(131, 91, 226, 0.35);
  background: rgba(131, 91, 226, 0.06);
}
.protocol-card:active:not(.disabled) {
  transform: scale(0.97);
}
.protocol-card.active {
  border-color: rgba(131, 91, 226, 0.6);
  background: linear-gradient(135deg, rgba(131, 91, 226, 0.18), rgba(94, 51, 193, 0.1));
}
[data-theme="dark"] .protocol-card.active .protocol-status { color: #c4b5fd; font-weight: 600; }
[data-theme="light"] .protocol-card.active .protocol-status { color: #7c3aed; font-weight: 600; }
.protocol-card.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.protocol-card.toggling {
  opacity: 0.6;
  pointer-events: none;
}
.protocol-icon {
  width: 28px;
  height: 28px;
  border-radius: 7px;
  background: rgba(131, 91, 226, 0.08);
  color: var(--text-dim);
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 6px;
}
[data-theme="dark"] .protocol-card.active .protocol-icon { background: rgba(131, 91, 226, 0.25); color: #a78bfa; }
[data-theme="light"] .protocol-card.active .protocol-icon { background: rgba(124, 58, 237, 0.1); color: #7c3aed; }
.protocol-name {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-bright);
  margin-bottom: 1px;
}
.protocol-status {
  font-size: 10px;
  color: var(--text-dim);
}

.device-name-row {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  text-align: center;
  padding: 2px 0;
}

.cast-panel {
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  border-radius: 14px;
  padding: 16px;
}
.media-section-title {
  font-size: 12px;
  font-weight: 700;
  color: #835be2;
  margin-bottom: 10px;
}
.media-info-grid {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.media-info-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.media-info-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-dim);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.media-info-value {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-bright);
  text-align: right;
}
.media-type-badge[data-type="video"] { color: #60a5fa; }
.media-type-badge[data-type="audio"] { color: #f472b6; }
.media-type-badge[data-type="image"] { color: #4ade80; }
.media-type-badge[data-type="document"] { color: #facc15; }
.state-badge[data-state="playing"] { color: #4ade80; font-weight: 600; }
.state-badge[data-state="paused"] { color: #fbbf24; }
.state-badge[data-state="loading"] { color: #60a5fa; }
.state-badge[data-state="idle"],
.state-badge[data-state="stopped"] { color: var(--text-muted); }
.state-badge[data-state="error"] { color: #f87171; font-weight: 600; }

.uri-text {
  font-size: 11px;
  color: var(--text-dim);
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: help;
}
.uri-copy-group {
  display: flex;
  align-items: center;
  gap: 4px;
}
.copy-btn {
  background: none;
  border: 1px solid var(--card-border);
  border-radius: 4px;
  color: var(--text-dim);
  font-size: 13px;
  width: 24px;
  height: 22px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  transition: all 0.2s;
  flex-shrink: 0;
}
.copy-btn:hover {
  background: rgba(131, 91, 226, 0.1);
  border-color: rgba(131, 91, 226, 0.3);
  color: #835be2;
}
.copy-btn:active {
  transform: scale(0.9);
}

.progress-group {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  min-width: 0;
}
.progress-bar {
  width: 100%;
  max-width: 220px;
  height: 4px;
  border-radius: 2px;
  background: var(--progress-track);
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #835be2, #5e33c1);
  border-radius: 2px;
  transition: width 0.4s ease;
}
.progress-time {
  font-size: 10px;
  color: var(--text-dim);
  font-variant-numeric: tabular-nums;
}
.progress-row {
  align-items: flex-start;
}

.hint-panel {
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  border-radius: 14px;
  padding: 28px 16px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.hint-logo {
  width: 64px;
  height: 64px;
  object-fit: contain;
  opacity: 0.7;
}
.hint-text {
  font-size: 13px;
  color: var(--text-dim);
  text-align: center;
}
</style>
