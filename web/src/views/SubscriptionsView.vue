<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Plus, RefreshCw, ExternalLink, Trash2, X, UserRound, AlertTriangle } from 'lucide-vue-next'
import { json, request } from '../api'
import type { Subscription } from '../types'

const items=ref<Subscription[]>([]),loading=ref(true),showAdd=ref(false),uploader=ref(''),saving=ref(false),error=ref('')
async function load(){loading.value=true;try{items.value=await request('/subscriptions')}catch(e){error.value=e instanceof Error?e.message:'加载失败'}finally{loading.value=false}}
async function add(){error.value='';saving.value=true;try{await request('/subscriptions',json('POST',{uploader:uploader.value}));uploader.value='';showAdd.value=false;await load()}catch(e){error.value=e instanceof Error?e.message:'添加失败'}finally{saving.value=false}}
async function toggle(item:Subscription){const previous=item.enabled;item.enabled=!item.enabled;try{await request(`/subscriptions/${item.id}`,json('PATCH',{enabled:item.enabled}))}catch(e){item.enabled=previous;error.value=e instanceof Error?e.message:'更新失败'}}
async function remove(item:Subscription){if(!confirm(`删除对“${item.name}”的订阅？`))return;try{await request(`/subscriptions/${item.id}`,{method:'DELETE'});items.value=items.value.filter(value=>value.id!==item.id)}catch(e){error.value=e instanceof Error?e.message:'删除失败'}}
function date(value:number|null){return value?new Intl.DateTimeFormat('zh-CN',{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(value*1000):'等待首次检查'}
onMounted(load)
</script>
<template>
  <section class="page">
    <header class="page-header"><div><p class="eyebrow">视频追踪</p><h1>UP 主订阅</h1><p>{{items.length}} 个订阅</p></div><div class="header-actions"><button class="icon-button" title="刷新" @click="load"><RefreshCw :size="19"/></button><button class="primary" @click="showAdd=true"><Plus :size="18"/>添加</button></div></header>
    <p v-if="error&&!showAdd" class="alert error"><AlertTriangle :size="17"/>{{error}}</p>
    <div v-if="loading" class="loading-state"><span class="spinner"/>正在加载</div>
    <div v-else-if="!items.length" class="empty-state"><UserRound :size="34"/><h2>还没有订阅</h2><button class="primary" @click="showAdd=true"><Plus :size="18"/>添加 UP 主</button></div>
    <div v-else class="subscription-list">
      <article v-for="item in items" :key="item.id" class="subscription-card" :class="{muted:!item.enabled}">
        <img :src="item.avatar" alt="" class="avatar"/>
        <div class="subscription-main"><div class="subscription-title"><h2>{{item.name}}</h2><span class="uid">UID {{item.mid}}</span></div><a v-if="item.latestBvid" :href="`https://www.bilibili.com/video/${item.latestBvid}`" target="_blank" rel="noreferrer">{{item.latestTitle}}<ExternalLink :size="14"/></a><p v-else>尚无公开视频</p><small :class="{danger:item.error}">{{item.error||`上次检查：${date(item.lastPolledAt)}`}}</small></div>
        <div class="row-actions"><label class="switch" :title="item.enabled?'暂停订阅':'启用订阅'"><input type="checkbox" :checked="item.enabled" @change="toggle(item)"/><span/></label><button class="icon-button danger-button" title="删除订阅" @click="remove(item)"><Trash2 :size="18"/></button></div>
      </article>
    </div>
  </section>
  <div v-if="showAdd" class="modal-backdrop" @click.self="showAdd=false"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="add-title"><header><div><p class="eyebrow">新订阅</p><h2 id="add-title">添加 UP 主</h2></div><button class="icon-button" title="关闭" @click="showAdd=false"><X :size="20"/></button></header><form @submit.prevent="add"><label>UID 或空间链接<input v-model.trim="uploader" placeholder="例如 546195" autofocus required /></label><p v-if="error" class="alert error">{{error}}</p><div class="modal-actions"><button type="button" class="secondary" @click="showAdd=false">取消</button><button class="primary" :disabled="saving"><Plus :size="18"/>{{saving?'正在验证':'添加订阅'}}</button></div></form></section></div>
</template>
