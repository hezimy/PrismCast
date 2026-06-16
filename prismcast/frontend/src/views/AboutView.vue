<template>
  <div class="about-view">
    <div class="about-card">
      <div class="logo-large"><img src="../assets/logo.png" alt="PrismCast" /></div>
      <h1 class="app-name">PrismCast</h1>
      <p class="app-tagline">{{ t('about.tagline') }}</p>
      <div class="app-version">{{ t('about.version') }} {{ deviceInfo.version }}</div>
      <div class="features">
        <div class="feature"><div class="feature-icon"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12.55a11 11 0 0114.08 0"/><path d="M8.53 16.11a6 6 0 016.95 0"/><circle cx="12" cy="20" r="1"/></svg></div><span>{{ t('about.featureDLNA') }}</span></div>
        <div class="feature"><div class="feature-icon"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg></div><span>{{ t('about.featurePlayback') }}</span></div>
        <div class="feature"><div class="feature-icon"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/></svg></div><span>{{ t('about.featureTray') }}</span></div>
      </div>
      <a class="github-link" href="https://github.com/hezimy/PrismCast" target="_blank" rel="noopener">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
        <span>GitHub</span>
      </a>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from '../i18n'
const { t } = useI18n()

defineProps({ deviceInfo: { type: Object, required: true } })
</script>

<style scoped>
.about-view { display: flex; flex-direction: column; align-items: center; padding-top: 10px; }
.about-card { background: var(--card-bg); border: 1px solid var(--card-border); border-radius: 16px; padding: 28px; text-align: center; max-width: 360px; width: 100%; }
.logo-large { width: 64px; height: 64px; margin: 0 auto 14px; }
.logo-large img { width: 100%; height: 100%; object-fit: contain; }
.app-name { font-size: 22px; font-weight: 700; background: linear-gradient(135deg, #835be2 0%, #5e33c1 50%, #3a197f 100%); -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text; margin-bottom: 6px; }
.app-tagline { font-size: 13px; color: var(--text-muted); margin-bottom: 8px; }
.app-version { font-size: 11px; color: var(--text-dim); font-family: 'SF Mono', monospace; margin-bottom: 16px; }
.features { display: flex; justify-content: center; gap: 10px; flex-wrap: wrap; margin-bottom: 16px; }
.feature { display: flex; align-items: center; gap: 5px; padding: 5px 10px; border-radius: 8px; background: rgba(131,91,226,0.08); font-size: 11px; color: #a78bfa; }
.feature-icon { display: flex; align-items: center; }
.github-link { display: inline-flex; align-items: center; gap: 6px; padding: 8px 16px; border-radius: 10px; background: rgba(131,91,226,0.1); color: #a78bfa; font-size: 13px; text-decoration: none; transition: all 0.2s ease; }
.github-link:hover { background: rgba(131,91,226,0.22); color: #c4b5fd; }
</style>
