import { createApp } from 'vue'
import App from './App.vue'
import { useI18n } from './i18n'

const app = createApp(App)

const { t, setLang, lang } = useI18n()
app.config.globalProperties.$t = t
app.config.globalProperties.$setLang = setLang
app.config.globalProperties.$lang = lang

app.mount('#app')
