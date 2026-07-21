<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { toCanvas } from 'qrcode'
import { AlertTriangle, CheckCircle2, Cookie, KeyRound, QrCode, Save, Send, ShieldCheck, Smartphone, X } from 'lucide-vue-next'
import { json, request } from '../api'
import type { Settings } from '../types'

interface QRStart { sessionId:string; qrUrl:string; expiresAt:number }
interface QRPoll { status:'waiting'|'scanned'|'expired'|'success'; message:string; name?:string }

const settings=ref<Settings|null>(null),cookie=ref(''),server=ref('https://api.day.app'),deviceKey=ref(''),level=ref('active'),sound=ref(''),message=ref(''),error=ref(''),busy=ref('')
const qrOpen=ref(false),qrSession=ref<QRStart|null>(null),qrStatus=ref(''),qrCanvas=ref<HTMLCanvasElement|null>(null)
let pollTimer:number|undefined

async function load(){try{const value=await request<Settings>('/settings');settings.value=value;server.value=value.bark.server;level.value=value.bark.level;sound.value=value.bark.sound}catch(e){error.value=e instanceof Error?e.message:'加载失败'}}
function feedback(ok:string,fail:unknown){if(ok){message.value=ok;error.value=''}else{message.value='';error.value=fail instanceof Error?fail.message:'操作失败'}}
async function saveCookie(){busy.value='cookie';try{await request('/settings/bilibili',json('PUT',{cookie:cookie.value}));cookie.value='';await load();feedback('B 站 Cookie 已验证并保存','')}catch(e){feedback('',e)}finally{busy.value=''}}
async function saveBark(){busy.value='bark';try{await request('/settings/bark',json('PUT',{server:server.value,deviceKey:deviceKey.value,level:level.value,sound:sound.value}));deviceKey.value='';await load();feedback('Bark 设置已保存','')}catch(e){feedback('',e)}finally{busy.value=''}}
async function testBark(){busy.value='test';try{await request('/settings/bark/test',{method:'POST'});feedback('测试通知已发送','')}catch(e){feedback('',e)}finally{busy.value=''}}
function time(value:number|null){return value?new Intl.DateTimeFormat('zh-CN',{dateStyle:'medium',timeStyle:'short'}).format(value*1000):'尚未验证'}
function biliSummary(value:Settings['bilibili']){return value.configured?`${value.name||value.status} · ${value.autoRefresh?'自动续期':'手动 Cookie'} · ${time(value.lastValidated)}`:'未配置'}

function stopPolling(){if(pollTimer!==undefined){window.clearTimeout(pollTimer);pollTimer=undefined}}
function schedulePoll(){stopPolling();pollTimer=window.setTimeout(pollQR,1500)}
async function startQR(){
  busy.value='qr';error.value='';qrStatus.value='正在生成二维码'
  try{
    qrSession.value=await request<QRStart>('/settings/bilibili/qrcode',{method:'POST'})
    qrOpen.value=true;qrStatus.value='请使用哔哩哔哩客户端扫码';await nextTick()
    if(qrCanvas.value)await toCanvas(qrCanvas.value,qrSession.value.qrUrl,{width:240,margin:1,color:{dark:'#171717',light:'#ffffff'}})
    schedulePoll()
  }catch(e){feedback('',e);qrOpen.value=false}
  finally{busy.value=''}
}
async function pollQR(){
  if(!qrOpen.value||!qrSession.value)return
  try{
    const result=await request<QRPoll>(`/settings/bilibili/qrcode/${qrSession.value.sessionId}/poll`,{method:'POST'})
    qrStatus.value=result.message
    if(result.status==='success'){
      stopPolling();qrOpen.value=false;qrSession.value=null;await load();feedback(`已登录 B 站账号 ${result.name||''}`.trim(),'');return
    }
    if(result.status==='expired'){stopPolling();return}
    schedulePoll()
  }catch(e){qrStatus.value=e instanceof Error?e.message:'扫码状态查询失败';schedulePoll()}
}
async function closeQR(){
  stopPolling();const session=qrSession.value;qrOpen.value=false;qrSession.value=null
  if(session)try{await request(`/settings/bilibili/qrcode/${session.sessionId}`,{method:'DELETE'})}catch{/* The session expires automatically. */}
}
onMounted(load)
onBeforeUnmount(()=>{stopPolling();if(qrSession.value)void request(`/settings/bilibili/qrcode/${qrSession.value.sessionId}`,{method:'DELETE'}).catch(()=>undefined)})
</script>

<template><section class="page narrow"><header class="page-header"><div><p class="eyebrow">个人配置</p><h1>设置</h1></div></header>
  <p v-if="message" class="alert success"><CheckCircle2 :size="17"/>{{message}}</p><p v-if="error" class="alert error"><AlertTriangle :size="17"/>{{error}}</p>
  <section class="settings-section"><header class="section-heading"><span class="section-icon coral"><Cookie :size="20"/></span><div><h2>B 站登录</h2><p v-if="settings"><span class="status-dot" :class="settings.bilibili.status"/>{{biliSummary(settings.bilibili)}}</p></div></header>
    <p v-if="settings?.bilibili.error" class="inline-error">{{settings.bilibili.error}}</p>
    <button class="primary" :disabled="busy==='qr'" @click="startQR"><QrCode :size="18"/>{{busy==='qr'?'正在生成':'扫码登录'}}</button>
    <details class="manual-cookie"><summary>手动填写 Cookie</summary><form @submit.prevent="saveCookie"><label>Cookie<textarea v-model="cookie" rows="4" :placeholder="settings?.bilibili.configured?'已安全保存；粘贴新 Cookie 可替换':'粘贴浏览器中的完整 Cookie'" required /></label><button class="secondary" :disabled="busy==='cookie'"><ShieldCheck :size="18"/>{{busy==='cookie'?'正在验证':'验证并保存'}}</button></form></details>
  </section>
  <section class="settings-section"><header class="section-heading"><span class="section-icon green"><Smartphone :size="20"/></span><div><h2>Bark 推送</h2><p>{{settings?.bark.configured?'已配置 Device Key':'尚未配置'}}</p></div></header><form @submit.prevent="saveBark"><label>Bark Server<input v-model.trim="server" type="url" required /></label><label>Device Key<input v-model.trim="deviceKey" type="password" :placeholder="settings?.bark.configured?'已安全保存；输入新 Key 可替换':'Bark 中显示的推送 Key'" required /></label><div class="form-grid"><label>通知级别<select v-model="level"><option value="active">主动通知</option><option value="timeSensitive">时效性通知</option><option value="passive">静默通知</option><option value="critical">重要警报</option></select></label><label>提示音（可选）<input v-model.trim="sound" placeholder="默认提示音" /></label></div><div class="button-row"><button class="primary" :disabled="busy==='bark'"><Save :size="18"/>保存 Bark</button><button type="button" class="secondary" :disabled="!settings?.bark.configured||busy==='test'" @click="testBark"><Send :size="18"/>发送测试</button></div></form></section>
  <section class="settings-section compact"><RouterLink to="/password" class="text-link"><KeyRound :size="18"/>修改登录密码</RouterLink></section>
  <div v-if="qrOpen" class="modal-backdrop" @click.self="closeQR"><section class="modal qr-modal" role="dialog" aria-modal="true" aria-labelledby="qr-title"><header><div><p class="eyebrow">B 站账号</p><h2 id="qr-title">扫码登录</h2></div><button class="icon-button" title="关闭" @click="closeQR"><X :size="20"/></button></header><div class="qr-canvas-wrap"><canvas ref="qrCanvas" width="240" height="240" role="img" aria-label="B 站登录二维码"/></div><p class="qr-status"><span class="status-dot" :class="qrStatus.includes('已扫码')?'valid':'pending'"/>{{qrStatus}}</p><button v-if="qrStatus.includes('过期')||qrStatus.includes('失败')" class="primary" @click="closeQR().then(startQR)"><QrCode :size="18"/>重新生成</button></section></div>
</section></template>
