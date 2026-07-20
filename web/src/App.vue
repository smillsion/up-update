<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { BellRing, ListVideo, History, Settings, Users, LogOut } from 'lucide-vue-next'
import { auth } from './api'
import { session } from './state'

const route=useRoute(),router=useRouter()
const publicPage=computed(()=>Boolean(route.meta.public))
const items=computed(()=>[
  {to:'/subscriptions',label:'订阅',icon:ListVideo},
  {to:'/history',label:'通知',icon:History},
  {to:'/settings',label:'设置',icon:Settings},
  ...(session.user?.role==='admin'?[{to:'/admin',label:'管理',icon:Users}]:[])
])
async function logout(){try{await auth.logout()}finally{session.user=null;router.replace('/login')}}
</script>

<template>
  <RouterView v-if="publicPage" />
  <div v-else class="app-shell">
    <aside class="sidebar">
      <div class="brand"><span class="brand-mark"><BellRing :size="20"/></span><span>up-update</span></div>
      <nav class="desktop-nav" aria-label="主导航">
        <RouterLink v-for="item in items" :key="item.to" :to="item.to"><component :is="item.icon" :size="19"/><span>{{item.label}}</span></RouterLink>
      </nav>
      <div class="account-block">
        <div><strong>{{session.user?.displayName}}</strong><small>@{{session.user?.username}}</small></div>
        <button class="icon-button" title="退出登录" aria-label="退出登录" @click="logout"><LogOut :size="18"/></button>
      </div>
    </aside>
    <main class="main-content"><RouterView/></main>
    <nav class="mobile-nav" aria-label="主导航">
      <RouterLink v-for="item in items" :key="item.to" :to="item.to"><component :is="item.icon" :size="21"/><span>{{item.label}}</span></RouterLink>
      <button title="退出登录" @click="logout"><LogOut :size="21"/><span>退出</span></button>
    </nav>
  </div>
</template>
