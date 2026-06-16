<template>
  <div class="devices-view">
    <div class="device-card">
      <div class="device-header">
        <div class="device-avatar"><svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg></div>
        <div class="device-meta"><div class="device-name">{{ deviceInfo.name }}</div><div class="device-type">DLNA Media Renderer</div></div>
        <div class="device-badge" :class="deviceInfo.status">{{ deviceInfo.status === 'running' ? '在线' : '离线' }}</div>
      </div>
      <div class="device-details">
        <div class="detail-row"><span class="detail-label">UUID</span><span class="detail-value">{{ deviceInfo.uuid }}</span></div>
        <div class="detail-row"><span class="detail-label">设备类型</span><span class="detail-value">MediaRenderer:1</span></div>
        <div class="detail-row"><span class="detail-label">版本</span><span class="detail-value">v{{ deviceInfo.version }}</span></div>
      </div>
    </div>
    <div class="protocols-grid">
      <div class="protocol-card active">
        <div class="protocol-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12.55a11 11 0 0114.08 0M1.42 9a16 16 0 0121.16 0M8.53 16.11a6 6 0 016.95 0"/></svg></div>
        <div class="protocol-name">DLNA/UPnP</div><div class="protocol-status">已启用</div>
      </div>
      <div class="protocol-card">
        <div class="protocol-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg></div>
        <div class="protocol-name">Chromecast</div><div class="protocol-status">计划中</div>
      </div>
      <div class="protocol-card">
        <div class="protocol-icon"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 17H4a2 2 0 01-2-2V5a2 2 0 012-2h16a2 2 0 012 2v10a2 2 0 01-2 2h-1"/><polygon points="12 15 17 21 7 21 12 15"/></svg></div>
        <div class="protocol-name">AirPlay</div><div class="protocol-status">计划中</div>
      </div>
    </div>
  </div>
</template>

<script setup>
defineProps({ deviceInfo: { type: Object, required: true } })
</script>

<style scoped>
.devices-view { display: flex; flex-direction: column; gap: 12px; }
.device-card { background: var(--card-bg); border: 1px solid var(--card-border); border-radius: 14px; padding: 16px; }
.device-header { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; padding-bottom: 12px; border-bottom: 1px solid var(--card-border); }
.device-avatar { width: 40px; height: 40px; border-radius: 10px; background: linear-gradient(135deg, rgba(131,91,226,0.2), rgba(94,51,193,0.15)); color: #a78bfa; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.device-name { font-size: 14px; font-weight: 600; color: var(--text-bright); }
.device-type { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
.device-badge { padding: 3px 10px; border-radius: 16px; font-size: 11px; font-weight: 500; background: rgba(239,68,68,0.15); color: #ef4444; }
.device-badge.running { background: rgba(16,185,129,0.15); color: #10b981; }
.device-details { display: flex; flex-direction: column; gap: 0; }
.detail-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 0; }
.detail-label { font-size: 12px; color: var(--text-muted); }
.detail-value { font-size: 12px; color: var(--text-bright); font-family: 'SF Mono', monospace; }

.protocols-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
.protocol-card { background: var(--card-bg); border: 1px solid var(--card-border); border-radius: 12px; padding: 14px; text-align: center; }
.protocol-card.active { border-color: rgba(131, 91, 226, 0.3); background: linear-gradient(135deg, rgba(131,91,226,0.1), rgba(94,51,193,0.05)); }
.protocol-icon { width: 32px; height: 32px; border-radius: 8px; background: rgba(131,91,226,0.1); color: #a78bfa; display: flex; align-items: center; justify-content: center; margin: 0 auto 8px; }
.protocol-card.active .protocol-icon { background: linear-gradient(135deg, rgba(131,91,226,0.2), rgba(94,51,193,0.15)); }
.protocol-name { font-size: 12px; font-weight: 600; color: var(--text-bright); margin-bottom: 2px; }
.protocol-status { font-size: 10px; color: var(--text-dim); }
.protocol-card.active .protocol-status { color: #a78bfa; }
</style>
