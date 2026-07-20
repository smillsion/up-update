import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { restoreSession } from './state'
import './style.css'

restoreSession().then(() => {
  createApp(App).use(router).mount('#app')
})
