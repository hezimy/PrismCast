<template>
  <div class="settings-view">
    <div class="settings-card">
      <div class="setting-row">
        <div class="setting-info"><div class="setting-name">{{ t('settings.deviceName') }}</div><div class="setting-desc">{{ t('settings.deviceNameDesc') }}</div></div>
        <input type="text" v-model="localSettings.device_name" class="setting-input" placeholder="PrismCast" />
      </div>
      <div class="setting-row">
        <div class="setting-info"><div class="setting-name">{{ t('settings.autoStart') }}</div></div>
        <label class="toggle"><input type="checkbox" v-model="localSettings.auto_start" /><span class="toggle-slider"></span></label>
      </div>
      <div class="setting-row">
        <div class="setting-info"><div class="setting-name">{{ t('settings.language') }}</div></div>
        <div class="custom-select" :class="{ open: openSelect === 'language' }" @click.stop="toggleSelect('language')">
          <div class="custom-select-display">
            <span class="custom-select-text">{{ languageLabel }}</span>
            <svg class="custom-select-arrow" width="10" height="6" viewBox="0 0 10 6"><path d="M0 0l5 6 5-6z" fill="#835be2"/></svg>
          </div>
          <div class="custom-select-dropdown" v-show="openSelect === 'language'">
            <div v-for="opt in languageOptions" :key="opt.value"
              class="custom-select-option" :class="{ active: localSettings.language === opt.value }"
              @click.stop="pickOption('language', opt.value)">{{ opt.label }}</div>
          </div>
        </div>
      </div>
      <div class="setting-row">
        <div class="setting-info"><div class="setting-name">{{ t('settings.theme') }}</div></div>
        <div class="custom-select" :class="{ open: openSelect === 'theme' }" @click.stop="toggleSelect('theme')">
          <div class="custom-select-display">
            <span class="custom-select-text">{{ themeLabel }}</span>
            <svg class="custom-select-arrow" width="10" height="6" viewBox="0 0 10 6"><path d="M0 0l5 6 5-6z" fill="#835be2"/></svg>
          </div>
          <div class="custom-select-dropdown" v-show="openSelect === 'theme'">
            <div v-for="opt in themeOptions" :key="opt.value"
              class="custom-select-option" :class="{ active: localSettings.theme === opt.value }"
              @click.stop="pickOption('theme', opt.value)">{{ opt.label }}</div>
          </div>
        </div>
      </div>
      <div class="setting-row">
        <div class="setting-info"><div class="setting-name">{{ t('settings.imageViewerFirst') }}</div><div class="setting-desc">{{ t('settings.imageViewerFirstDesc') }}</div></div>
        <label class="toggle"><input type="checkbox" v-model="localSettings.image_viewer_first" /><span class="toggle-slider"></span></label>
      </div>
      <div class="setting-row setting-row-compact">
        <div class="setting-info"><div class="setting-name">{{ t('settings.logLevel') }}</div></div>
        <div class="setting-controls">
          <div class="custom-select log-select" :class="{ open: openSelect === 'log_level' }" @click.stop="toggleSelect('log_level')">
            <div class="custom-select-display">
              <span class="custom-select-text">{{ logLevelLabel }}</span>
              <svg class="custom-select-arrow" width="10" height="6" viewBox="0 0 10 6"><path d="M0 0l5 6 5-6z" fill="#835be2"/></svg>
            </div>
            <div class="custom-select-dropdown" v-show="openSelect === 'log_level'">
              <div v-for="opt in logLevelOptions" :key="opt.value"
                class="custom-select-option" :class="{ active: localSettings.log_level === opt.value }"
                @click.stop="pickOption('log_level', opt.value)">{{ opt.label }}</div>
            </div>
          </div>
          <button type="button" class="btn-open-log" @click.stop="openLogFolder">{{ t('settings.openLogFolder') }}</button>
        </div>
      </div>
    </div>
    <div class="settings-actions">
      <button class="btn-save" @click="save">{{ t('settings.save') }}</button>
      <button class="btn-reset" @click="reset">{{ t('settings.reset') }}</button>
      <span v-if="saveHint" class="save-hint" :class="{ err: saveFailed }">{{ saveHint }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from '../i18n'
import { OpenLogFolder } from '../../wailsjs/go/main/App'

const { t } = useI18n()

const props = defineProps({
  settings: { type: Object, required: true },
  save: { type: Function, required: true },
})

const localSettings = ref({ ...props.settings })
const openSelect = ref(null)
const saveHint = ref('')
const saveFailed = ref(false)

const languageOptions = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en-US', label: 'English' },
]
const themeOptions = computed(() => [
  { value: 'dark', label: t('settings.themeDark') },
  { value: 'light', label: t('settings.themeLight') },
])
const logLevelOptions = computed(() => [
  { value: 'main', label: t('settings.logMain') },
  { value: 'verbose', label: t('settings.logVerbose') },
  { value: 'off', label: t('settings.logOff') },
])

const languageLabel = computed(() => languageOptions.find(o => o.value === localSettings.value.language)?.label || '')
const themeLabel = computed(() => themeOptions.value.find(o => o.value === localSettings.value.theme)?.label || '')
const logLevelLabel = computed(() => logLevelOptions.value.find(o => o.value === localSettings.value.log_level)?.label || '')

function toggleSelect(name) { openSelect.value = openSelect.value === name ? null : name }
function pickOption(key, value) {
  localSettings.value[key] = value
  openSelect.value = null
}

function onDocClick() { openSelect.value = null }

onMounted(() => {
  document.addEventListener('click', onDocClick)
  localSettings.value = { ...props.settings, log_level: normalizeLogLevel(props.settings) }
})
onUnmounted(() => document.removeEventListener('click', onDocClick))

function normalizeLogLevel(s) {
  if (s.log_level) return s.log_level
  if (s.verbose_log) return 'verbose'
  return 'main'
}

async function openLogFolder() {
  saveHint.value = ''
  saveFailed.value = false
  try {
    await OpenLogFolder()
  } catch (e) {
    console.error('Open log folder failed:', e)
    saveFailed.value = true
    saveHint.value = t('settings.openLogFolderFailed')
  }
}

async function save() {
  saveHint.value = ''
  saveFailed.value = false
  const payload = { ...localSettings.value }
  delete payload.verbose_log
  try {
    await props.save(payload)
    saveHint.value = t('settings.saved')
    setTimeout(() => { saveHint.value = '' }, 2500)
  } catch (e) {
    console.error('Save failed:', e)
    saveFailed.value = true
    saveHint.value = t('settings.saveFailed')
  }
}
function reset() {
  localSettings.value = { ...props.settings, log_level: normalizeLogLevel(props.settings) }
}
</script>

<style scoped>
.settings-view { display: flex; flex-direction: column; gap: 10px; max-width: 600px; height: 100%; overflow-y: auto; padding-right: 4px; }
.settings-view::-webkit-scrollbar { width: 5px; }
.settings-view::-webkit-scrollbar-track { background: transparent; }
.settings-view::-webkit-scrollbar-thumb { background: rgba(131,91,226,0.25); border-radius: 3px; }
.settings-view::-webkit-scrollbar-thumb:hover { background: rgba(131,91,226,0.4); }
.settings-card { background: var(--card-bg); border: 1px solid var(--card-border); border-radius: 14px; padding: 2px 16px; }
.setting-row { display: flex; align-items: center; justify-content: space-between; padding: 7px 0; border-bottom: 1px solid var(--card-border); gap: 12px; }
.setting-row-compact { padding: 6px 0; }
.setting-row:last-child { border-bottom: none; }
.setting-info { flex: 1; min-width: 0; }
.setting-name { font-size: 13px; font-weight: 500; color: var(--text-bright); }
.setting-desc { font-size: 11px; color: var(--text-dim); margin-top: 1px; }
.setting-controls { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }

.setting-input { width: 180px; padding: 6px 10px; border-radius: 8px; border: 1px solid rgba(131,91,226,0.25); background: var(--input-bg); color: var(--text-bright); font-size: 13px; outline: none; transition: border-color 0.2s, box-shadow 0.2s; box-sizing: border-box; }
.setting-input:hover { border-color: rgba(131,91,226,0.4); }
.setting-input:focus { border-color: #835be2; box-shadow: 0 0 0 3px rgba(131,91,226,0.15); }
.setting-input::placeholder { color: var(--text-dim); }

.custom-select { position: relative; width: 180px; user-select: none; }
.log-select { width: 120px; }
.custom-select-display { display: flex; align-items: center; justify-content: space-between; padding: 6px 10px; border-radius: 8px; border: 1px solid rgba(131,91,226,0.25); background: var(--input-bg); cursor: pointer; transition: border-color 0.2s, box-shadow 0.2s; min-height: 32px; box-sizing: border-box; }
.custom-select-display:hover { border-color: rgba(131,91,226,0.4); }
.custom-select.open .custom-select-display { border-color: #835be2; box-shadow: 0 0 0 3px rgba(131,91,226,0.15); }
.custom-select-text { font-size: 13px; color: var(--text-bright); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.custom-select-arrow { flex-shrink: 0; margin-left: 6px; transition: transform 0.2s ease; }
.custom-select.open .custom-select-arrow { transform: rotate(180deg); }

.custom-select-dropdown { position: absolute; top: calc(100% + 4px); left: 0; right: 0; z-index: 100; border-radius: 8px; border: 1px solid rgba(131,91,226,0.25); background: var(--bg-primary); box-shadow: 0 8px 24px rgba(0,0,0,0.15), 0 0 0 1px rgba(131,91,226,0.08); overflow: hidden; padding: 4px 0; }
.custom-select-option { padding: 7px 12px; font-size: 13px; color: var(--text-secondary); cursor: pointer; transition: all 0.15s; white-space: nowrap; }
.custom-select-option:hover { background: rgba(131,91,226,0.1); color: #a78bfa; }
.custom-select-option.active { background: rgba(131,91,226,0.15); color: #835be2; font-weight: 500; }

.toggle { position: relative; width: 40px; height: 22px; cursor: pointer; flex-shrink: 0; }
.toggle input { opacity: 0; width: 0; height: 0; }
.toggle-slider { position: absolute; top: 0; left: 0; right: 0; bottom: 0; background: rgba(131,91,226,0.15); border-radius: 22px; transition: all 0.3s ease; }
.toggle-slider::before { content: ''; position: absolute; height: 16px; width: 16px; left: 3px; bottom: 3px; background: var(--text-muted); border-radius: 50%; transition: all 0.3s ease; }
.toggle input:checked + .toggle-slider { background: linear-gradient(135deg, #835be2, #5e33c1); }
.toggle input:checked + .toggle-slider::before { transform: translateX(18px); background: #fff; }

.settings-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.save-hint { font-size: 12px; color: #7c3aed; }
.save-hint.err { color: #ef4444; }
.btn-open-log {
  padding: 6px 10px;
  border-radius: 8px;
  border: 1px solid rgba(131,91,226,0.25);
  background: rgba(131,91,226,0.08);
  color: var(--text-bright);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
  flex-shrink: 0;
  white-space: nowrap;
}
.btn-open-log:hover { background: rgba(131,91,226,0.16); border-color: rgba(131,91,226,0.4); }
.btn-save { padding: 8px 20px; border-radius: 10px; border: none; background: linear-gradient(135deg, #835be2, #5e33c1); color: white; font-size: 13px; font-weight: 500; cursor: pointer; transition: all 0.2s ease; }
.btn-save:hover { filter: brightness(1.08); box-shadow: 0 0 0 1px rgba(131,91,226,0.35); }
.btn-reset { padding: 8px 20px; border-radius: 10px; border: 1px solid rgba(131,91,226,0.2); background: transparent; color: var(--text-muted); font-size: 13px; font-weight: 500; cursor: pointer; transition: all 0.2s ease; flex-shrink: 0; }
.btn-reset:hover { background: rgba(131,91,226,0.08); color: var(--text-bright); border-color: rgba(131,91,226,0.3); }
</style>
