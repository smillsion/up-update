<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { AlertTriangle, ChevronLeft, ChevronRight, ExternalLink, ListPlus, Plus, RefreshCw, Trash2, UserRound, X } from 'lucide-vue-next'
import { json, request } from '../api'
import type { Following, FollowingImportResult, PageResult, Settings, Subscription } from '../types'

const items=ref<Subscription[]>([]),settings=ref<Settings|null>(null),loading=ref(true),showAdd=ref(false),showImport=ref(false)
const uploader=ref(''),saving=ref(false),error=ref(''),message=ref('')
const followings=ref<PageResult<Following>|null>(null),followingLoading=ref(false),followingError=ref(''),selected=ref<string[]>([])
const cookieReady=computed(()=>settings.value?.bilibili.configured&&settings.value.bilibili.status==='valid')
const initializing=computed(()=>items.value.some(item=>!item.latestBvid&&!item.lastPolledAt&&!item.error))
const initializationAttempts=ref(0)
let initializationTimer:ReturnType<typeof setTimeout>|undefined

function stopInitializationRefresh(){if(initializationTimer){clearTimeout(initializationTimer);initializationTimer=undefined}}
function scheduleInitializationRefresh(){stopInitializationRefresh();if(!initializing.value||initializationAttempts.value>=12)return;initializationTimer=setTimeout(()=>{initializationAttempts.value++;void load(true)},5000)}
async function load(background=false){if(!background)loading.value=true;error.value='';try{const [subscriptions,userSettings]=await Promise.all([request<Subscription[]>('/subscriptions'),request<Settings>('/settings')]);items.value=subscriptions;settings.value=userSettings}catch(e){error.value=e instanceof Error?e.message:'加载失败'}finally{if(!background)loading.value=false;scheduleInitializationRefresh()}}
function refresh(){initializationAttempts.value=0;void load()}
async function add(){error.value='';saving.value=true;try{await request('/subscriptions',json('POST',{uploader:uploader.value}));uploader.value='';showAdd.value=false;message.value='订阅已添加';await load()}catch(e){error.value=e instanceof Error?e.message:'添加失败'}finally{saving.value=false}}
async function toggle(item:Subscription){const previous=item.enabled;item.enabled=!item.enabled;try{await request(`/subscriptions/${item.id}`,json('PATCH',{enabled:item.enabled}))}catch(e){item.enabled=previous;error.value=e instanceof Error?e.message:'更新失败'}}
async function remove(item:Subscription){if(!confirm(`删除对“${item.name}”的订阅？`))return;try{await request(`/subscriptions/${item.id}`,{method:'DELETE'});items.value=items.value.filter(value=>value.id!==item.id)}catch(e){error.value=e instanceof Error?e.message:'删除失败'}}
async function openImport(){showImport.value=true;selected.value=[];await loadFollowings(1)}
async function loadFollowings(page:number){followingLoading.value=true;followingError.value='';selected.value=[];try{followings.value=await request<PageResult<Following>>(`/subscriptions/followings?page=${page}`)}catch(e){followingError.value=e instanceof Error?e.message:'无法读取关注列表'}finally{followingLoading.value=false}}
function selectFollowing(item:Following,event:Event){const input=event.target as HTMLInputElement;if(input.checked){if(selected.value.length>=20){input.checked=false;followingError.value='每次最多选择 20 个关注账号';return}selected.value=[...selected.value,item.mid]}else selected.value=selected.value.filter(mid=>mid!==item.mid)}
async function importSelected(){if(!followings.value||!selected.value.length)return;saving.value=true;followingError.value='';try{const result=await request<FollowingImportResult>('/subscriptions/import-followings',json('POST',{page:followings.value.page,mids:selected.value}));showImport.value=false;message.value=`已导入 ${result.imported} 个订阅${result.skipped?`，跳过 ${result.skipped} 个已订阅账号`:''}${result.pending?`，${result.pending} 个正在后台获取最新投稿`:''}`;initializationAttempts.value=0;await load()}catch(e){followingError.value=e instanceof Error?e.message:'导入失败'}finally{saving.value=false}}
function date(value:number|null){return value?new Intl.DateTimeFormat('zh-CN',{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(value*1000):'等待首次检查'}
onMounted(()=>load())
onBeforeUnmount(stopInitializationRefresh)
</script>

<template>
  <section class="page">
    <header class="page-header"><div><p class="eyebrow">视频追踪</p><h1>UP 主订阅</h1><p>{{items.length}} 个订阅</p></div><div class="header-actions"><button class="icon-button" title="刷新" @click="refresh"><RefreshCw :size="19"/></button><button class="icon-button" title="从关注导入" :disabled="!cookieReady" @click="openImport"><ListPlus :size="19"/></button><button class="primary" :disabled="!cookieReady" @click="showAdd=true"><Plus :size="18"/>添加</button></div></header>
    <p v-if="settings&&!cookieReady" class="alert warning"><AlertTriangle :size="17"/><span>订阅功能需要有效的 B 站 Cookie</span><RouterLink to="/settings">前往设置</RouterLink></p>
    <p v-if="message" class="alert success">{{message}}</p>
    <p v-if="error&&!showAdd" class="alert error"><AlertTriangle :size="17"/>{{error}}</p>
    <div v-if="loading" class="loading-state"><span class="spinner"/>正在加载</div>
    <div v-else-if="!items.length" class="empty-state"><UserRound :size="34"/><h2>还没有订阅</h2><button class="primary" :disabled="!cookieReady" @click="showAdd=true"><Plus :size="18"/>添加 UP 主</button></div>
    <div v-else class="subscription-list">
      <article v-for="item in items" :key="item.id" class="subscription-card" :class="{muted:!item.enabled}">
        <img :src="item.avatar" alt="" class="avatar"/>
        <div class="subscription-main"><div class="subscription-title"><h2>{{item.name}}</h2><span class="uid">UID {{item.mid}}</span></div><a v-if="item.latestBvid" :href="`https://www.bilibili.com/video/${item.latestBvid}`" target="_blank" rel="noreferrer">{{item.latestTitle}}<ExternalLink :size="14"/></a><p v-else-if="!item.lastPolledAt&&!item.error">正在获取最新投稿</p><p v-else>尚无公开视频</p><small :class="{danger:item.error}">{{item.error||`上次检查：${date(item.lastPolledAt)}`}}</small></div>
        <div class="row-actions"><label class="switch" :title="item.enabled?'暂停订阅':'启用订阅'"><input type="checkbox" :checked="item.enabled" @change="toggle(item)"/><span/></label><button class="icon-button danger-button" title="删除订阅" @click="remove(item)"><Trash2 :size="18"/></button></div>
      </article>
    </div>
  </section>

  <div v-if="showAdd" class="modal-backdrop" @click.self="showAdd=false"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="add-title"><header><div><p class="eyebrow">新订阅</p><h2 id="add-title">添加 UP 主</h2></div><button class="icon-button" title="关闭" @click="showAdd=false"><X :size="20"/></button></header><form @submit.prevent="add"><label>UID 或空间链接<input v-model.trim="uploader" placeholder="例如 546195" autofocus required /></label><p v-if="error" class="alert error">{{error}}</p><div class="modal-actions"><button type="button" class="secondary" @click="showAdd=false">取消</button><button class="primary" :disabled="saving"><Plus :size="18"/>{{saving?'正在验证':'添加订阅'}}</button></div></form></section></div>

  <div v-if="showImport" class="modal-backdrop" @click.self="showImport=false"><section class="modal wide" role="dialog" aria-modal="true" aria-labelledby="import-title"><header><div><p class="eyebrow">B 站关注</p><h2 id="import-title">从关注列表导入</h2></div><button class="icon-button" title="关闭" @click="showImport=false"><X :size="20"/></button></header><p v-if="followingError" class="alert error">{{followingError}}</p><div v-if="followingLoading" class="following-loading"><span class="spinner"/>正在读取关注列表</div><div v-else-if="followings&&!followings.items.length" class="following-empty">这一页没有关注账号</div><div v-else-if="followings" class="following-list"><label v-for="item in followings.items" :key="item.mid" class="following-row" :class="{disabled:item.subscribed}"><img :src="item.avatar" alt="" class="avatar small"/><span><strong>{{item.name}}</strong><small>UID {{item.mid}}</small></span><em v-if="item.subscribed">已订阅</em><input v-else type="checkbox" :checked="selected.includes(item.mid)" @change="selectFollowing(item,$event)"/></label></div><div v-if="followings" class="following-toolbar"><div class="pager compact-pager"><button class="icon-button compact" title="上一页" :disabled="followingLoading||followings.page<=1" @click="loadFollowings(followings.page-1)"><ChevronLeft :size="18"/></button><span>{{followings.page}} / {{followings.totalPages||1}}</span><button class="icon-button compact" title="下一页" :disabled="followingLoading||followings.page>=followings.totalPages" @click="loadFollowings(followings.page+1)"><ChevronRight :size="18"/></button></div><span>已选 {{selected.length}} / 20</span></div><div class="modal-actions"><button type="button" class="secondary" @click="showImport=false">取消</button><button class="primary" :disabled="saving||!selected.length" @click="importSelected"><ListPlus :size="18"/>{{saving?'正在导入':`导入选中 (${selected.length})`}}</button></div></section></div>
</template>
