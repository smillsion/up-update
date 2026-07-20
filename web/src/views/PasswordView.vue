<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { KeyRound } from 'lucide-vue-next'
import { json, request } from '../api'
import { session } from '../state'
const router=useRouter(),current=ref(''),next=ref(''),confirm=ref(''),message=ref(''),error=ref(''),loading=ref(false)
async function submit(){error.value='';if(next.value!==confirm.value){error.value='两次输入的新密码不一致';return}loading.value=true;try{await request('/auth/password',json('PUT',{currentPassword:current.value,newPassword:next.value}));if(session.user)session.user.forcePasswordChange=false;message.value='密码已更新';setTimeout(()=>router.replace('/subscriptions'),500)}catch(e){error.value=e instanceof Error?e.message:'修改失败'}finally{loading.value=false}}
</script>
<template><section class="page narrow"><header class="page-header"><div><p class="eyebrow">账号安全</p><h1>{{session.user?.forcePasswordChange?'修改临时密码':'修改密码'}}</h1></div></header><form class="settings-section" @submit.prevent="submit"><label>当前密码<input v-model="current" type="password" autocomplete="current-password" required /></label><label>新密码<input v-model="next" type="password" minlength="10" autocomplete="new-password" required /><small>至少 10 个字符</small></label><label>确认新密码<input v-model="confirm" type="password" autocomplete="new-password" required /></label><p v-if="error" class="alert error">{{error}}</p><p v-if="message" class="alert success">{{message}}</p><button class="primary" :disabled="loading"><KeyRound :size="18"/>保存密码</button></form></section></template>
