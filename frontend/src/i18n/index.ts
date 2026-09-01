import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import zh from './locales/zh.json'
import en from './locales/en.json'

function detectLang(): string {
  const saved = localStorage.getItem('homihub-lang')
  if (saved) return saved
  const nav = (navigator.language || 'zh').toLowerCase()
  if (nav.startsWith('zh')) return 'zh'
  return 'en'
}

i18n.use(initReactI18next).init({
  resources: {
    zh: { translation: zh },
    en: { translation: en },
  },
  lng: detectLang(),
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

export default i18n
export function setLang(lng: string) {
  localStorage.setItem('homihub-lang', lng)
  void i18n.changeLanguage(lng)
}
