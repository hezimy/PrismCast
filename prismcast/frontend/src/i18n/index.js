import { ref, computed } from 'vue'
import { messages } from './messages'

const currentLang = ref('zh-CN')

export function useI18n() {
  const t = (key) => {
    const keys = key.split('.')
    let value = messages[currentLang.value]
    for (const k of keys) {
      if (value && typeof value === 'object') {
        value = value[k]
      } else {
        return key
      }
    }
    return typeof value === 'string' ? value : key
  }

  const setLang = (lang) => {
    if (messages[lang]) {
      currentLang.value = lang
    }
  }

  const lang = computed(() => currentLang.value)

  return { t, setLang, lang }
}

export { messages }
