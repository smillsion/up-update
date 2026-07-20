<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Check, Clock3, ExternalLink, RefreshCw, XCircle, BellOff } from 'lucide-vue-next'
import { request } from '../api'
import type { Delivery } from '../types'
const items=ref<Delivery[]>([]),loading=ref(true),error=ref('')
async function load(){loading.value=true;try{items.value=await request('/deliveries')}catch(e){error.value=e instanceof Error?e.message:'加载失败'}finally{loading.value=false}}
function date(value:number){return new Intl.DateTimeFormat('zh-CN',{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(value*1000)}
const status={sent:{label:'已送达',icon:Check},pending:{label:'等待发送',icon:Clock3},failed:{label:'发送失败',icon:XCircle}}
onMounted(load)
</script>
<template><section class="page"><header class="page-header"><div><p class="eyebrow">投递记录</p><h1>通知</h1><p>最近 100 条</p></div><button class="icon-button" title="刷新" @click="load"><RefreshCw :size="19"/></button></header><p v-if="error" class="alert error">{{error}}</p><div v-if="loading" class="loading-state"><span class="spinner"/>正在加载</div><div v-else-if="!items.length" class="empty-state"><BellOff :size="34"/><h2>还没有通知</h2></div><div v-else class="history-list"><a v-for="item in items" :key="item.id" :href="item.videoUrl" target="_blank" rel="noreferrer" class="history-row"><img :src="item.creatorAvatar" alt="" class="avatar small"/><div class="history-main"><div><strong>{{item.creatorName}}</strong><span class="delivery-status" :class="item.status"><component :is="status[item.status].icon" :size="14"/>{{status[item.status].label}}</span></div><p>{{item.videoTitle}}</p><small>{{date(item.createdAt)}}<span v-if="item.error"> · {{item.error}}</span></small></div><ExternalLink :size="17"/></a></div></section></template>
