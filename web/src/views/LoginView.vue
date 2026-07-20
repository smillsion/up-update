<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { BellRing, LogIn } from 'lucide-vue-next'
import { auth } from '../api'
import { session } from '../state'
const router=useRouter(),username=ref(''),password=ref(''),error=ref(''),loading=ref(false)
async function submit(){error.value='';loading.value=true;try{session.user=await auth.login(username.value,password.value);await router.replace(session.user.forcePasswordChange?'/password':'/subscriptions')}catch(e){error.value=e instanceof Error?e.message:'登录失败'}finally{loading.value=false}}
</script>
<template>
  <main class="login-page">
    <section class="login-panel">
      <div class="login-brand"><span class="brand-mark large"><BellRing :size="26"/></span><div><h1>up-update</h1><p>登录你的推送中心</p></div></div>
      <form @submit.prevent="submit">
        <label>用户名<input v-model.trim="username" autocomplete="username" autofocus required /></label>
        <label>密码<input v-model="password" type="password" autocomplete="current-password" required /></label>
        <p v-if="error" class="alert error">{{error}}</p>
        <button class="primary wide" :disabled="loading"><LogIn :size="18"/>{{loading?'正在登录':'登录'}}</button>
      </form>
    </section>
  </main>
</template>
