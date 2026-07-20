<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { BellOff, Check, Clock3, ExternalLink, RefreshCw, Trash2, XCircle } from 'lucide-vue-next'
import { request } from '../api'
import type { Delivery } from '../types'
const items=ref<Delivery[]>([]),loading=ref(true),error=ref(''),removing=ref<number|null>(null)
async function load(){loading.value=true;try{items.value=await request('/deliveries')}catch(e){error.value=e instanceof Error?e.message:'加载失败'}finally{loading.value=false}}
async function remove(item:Delivery){if(!confirm(`取消“${item.videoTitle}”的等待发送通知？`))return;removing.value=item.id;error.value='';try{await request(`/deliveries/${item.id}`,{method:'DELETE'});items.value=items.value.filter(value=>value.id!==item.id)}catch(e){error.value=e instanceof Error?e.message:'取消发送失败';await load()}finally{removing.value=null}}
function date(value:number){return new Intl.DateTimeFormat('zh-CN',{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(value*1000)}
const status={sent:{label:'已送达',icon:Check},pending:{label:'等待发送',icon:Clock3},failed:{label:'发送失败',icon:XCircle}}
onMounted(load)
</script>
<template><section class="page"><header class="page-header"><div><p class="eyebrow">投递记录</p><h1>通知</h1><p>最近 100 条</p></div><button class="icon-button" title="刷新" @click="load"><RefreshCw :size="19"/></button></header><p v-if="error" class="alert error">{{error}}</p><div v-if="loading" class="loading-state"><span class="spinner"/>正在加载</div><div v-else-if="!items.length" class="empty-state"><BellOff :size="34"/><h2>还没有通知</h2></div><div v-else class="history-list"><article v-for="item in items" :key="item.id" class="history-row"><img :src="item.creatorAvatar" alt="" class="avatar small"/><a :href="item.videoUrl" target="_blank" rel="noreferrer" class="history-main"><div><strong>{{item.creatorName}}</strong><span class="delivery-status" :class="item.status"><component :is="status[item.status].icon" :size="14"/>{{status[item.status].label}}</span></div><p>{{item.videoTitle}}</p><small>{{date(item.createdAt)}}<span v-if="item.error"> · {{item.error}}</span></small></a><div class="history-actions"><a :href="item.videoUrl" target="_blank" rel="noreferrer" class="icon-button compact" title="打开视频"><ExternalLink :size="17"/></a><button v-if="item.status==='pending'" class="icon-button compact danger-button" title="取消发送" :disabled="removing===item.id" @click="remove(item)"><Trash2 :size="17"/></button></div></article></div></section></template>
