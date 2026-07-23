<script setup lang="ts">
import { computed } from 'vue'
import { BellRing, Check, Clock3, Code2, Container, Github, ListVideo, LogIn, QrCode, ShieldCheck, Smartphone, Users } from 'lucide-vue-next'
import { session } from '../state'
import '../home.css'

const authenticated=computed(()=>Boolean(session.user))
const primaryTarget=computed(()=>authenticated.value?'/subscriptions':'/login')
const primaryLabel=computed(()=>authenticated.value?'进入控制台':'登录系统')
const appIcon='/app-icon.png'
const features=[
  {icon:ListVideo,title:'精准订阅',body:'只推送你主动选择的 UP 主新投稿，不必开启 B 站 App 消息通知。'},
  {icon:Users,title:'独立配置',body:'家人朋友使用自己的系统账号、UP 主订阅列表和 Bark Device Key。'},
  {icon:QrCode,title:'可选扫码登录',body:'UID 可直接订阅；扫码可导入关注列表，并提升投稿轮询稳定性。'},
  {icon:Clock3,title:'分时轮询',body:'睡眠、工作和空闲时间使用不同检查频率，兼顾及时性与稳定性。'},
  {icon:BellRing,title:'原生推送',body:'支持官方或自建 Bark、提示音、通知级别以及延迟补发。'},
  {icon:ShieldCheck,title:'本地存储',body:'SQLite 单实例存储，敏感凭证使用 AES-256-GCM 加密。'},
]
</script>

<template>
  <div class="home-page">
    <section class="home-hero">
      <div class="home-hero-overlay">
        <header class="home-header">
          <RouterLink to="/about" class="home-brand" aria-label="up-update 项目主页"><img :src="appIcon" alt=""/><strong>up-update</strong></RouterLink>
          <nav class="home-header-links" aria-label="主页导航">
            <a href="#features">主要特性</a>
            <a href="#preview">界面预览</a>
            <a href="#deploy">快速部署</a>
            <a href="https://github.com/smillsion/up-update" target="_blank" rel="noreferrer">GitHub</a>
          </nav>
          <RouterLink :to="primaryTarget" class="home-header-action">{{primaryLabel}}</RouterLink>
        </header>

        <div class="home-hero-content">
          <img class="home-hero-logo" :src="appIcon" alt="up-update Logo"/>
          <p class="home-kicker">B 站 UP 主更新提醒</p>
          <h1>up-update</h1>
          <p class="home-lead">只关注你真正关心的 UP 主。新投稿通过 Bark 直接推送到 iPhone，无需开启 B 站 App 的消息通知，也能避开无关内容的频繁打扰。</p>
          <div class="home-actions">
            <RouterLink :to="primaryTarget" class="home-button primary"><LogIn :size="19"/>{{primaryLabel}}</RouterLink>
            <a class="home-button secondary" href="https://gitee.com/birdKiss/up-update/blob/main/docs/DEPLOYMENT.md" target="_blank" rel="noreferrer"><Container :size="19"/>部署指南</a>
          </div>
          <div class="home-assurances" aria-label="项目特点">
            <span><Check :size="16"/>多用户独立配置</span>
            <span><Check :size="16"/>Docker 自托管</span>
            <span><Check :size="16"/>简体中文移动端</span>
          </div>
        </div>
      </div>
    </section>

    <main>
      <section id="features" class="home-section home-features">
        <div class="home-section-heading">
          <p>为什么使用 up-update</p>
          <h2>把提醒范围交还给你自己</h2>
          <span>选择值得打断你的更新，其余内容继续安静地留在 B 站。</span>
        </div>
        <div class="home-feature-grid">
          <article v-for="item in features" :key="item.title" class="home-feature">
            <component :is="item.icon" :size="23"/>
            <h3>{{item.title}}</h3>
            <p>{{item.body}}</p>
          </article>
        </div>
      </section>

      <section class="home-flow-band">
        <div class="home-section home-flow">
          <div class="home-section-heading compact">
            <p>工作方式</p>
            <h2>从选择到推送，四步完成</h2>
          </div>
          <ol>
            <li><span>01</span><strong>配置</strong><p>登录系统并填写 Bark 推送信息</p></li>
            <li><span>02</span><strong>选择</strong><p>添加 UID 或从关注列表导入</p></li>
            <li><span>03</span><strong>监控</strong><p>后台按分时策略检查新投稿</p></li>
            <li><span>04</span><strong>推送</strong><p>Bark 将更新送到你的 iPhone</p></li>
          </ol>
        </div>
      </section>

      <section id="preview" class="home-section home-preview">
        <div class="home-section-heading">
          <p>实际界面</p>
          <h2>清楚、克制，适合每天使用</h2>
          <span>桌面端管理订阅，移动端完成登录和推送设置；所有用户数据彼此隔离。</span>
        </div>
        <div class="home-product-shots">
          <figure class="home-desktop-shot"><img :src="'/home/subscriptions-desktop.png'" alt="up-update 桌面端 UP 主订阅界面" loading="lazy"/><figcaption>桌面端订阅管理</figcaption></figure>
          <figure class="home-mobile-shot"><img :src="'/home/settings-mobile.png'" alt="up-update 移动端个人设置界面" loading="lazy"/><figcaption>移动端个人配置</figcaption></figure>
        </div>
      </section>

      <section id="deploy" class="home-deploy-band">
        <div class="home-section home-deploy">
          <div>
            <p class="home-kicker">Docker 部署</p>
            <h2>在自己的服务器上运行</h2>
            <p>一个容器、一个 SQLite 数据卷。支持 Docker Compose，也支持无 Compose 的部署脚本。</p>
            <div class="home-repo-links">
              <a href="https://github.com/smillsion/up-update" target="_blank" rel="noreferrer"><Github :size="18"/>GitHub</a>
              <a href="https://gitee.com/birdKiss/up-update" target="_blank" rel="noreferrer"><Code2 :size="18"/>Gitee</a>
            </div>
          </div>
          <pre><code>git clone https://gitee.com/birdKiss/up-update.git
cd up-update
cp .env.example .env
docker compose up -d --build</code></pre>
        </div>
      </section>
    </main>

    <footer class="home-footer">
      <div><img :src="appIcon" alt=""/><span><strong>up-update</strong><small>只推送你关心的 UP 主更新</small></span></div>
      <p>MIT License · 简体中文 · 自托管</p>
    </footer>
  </div>
</template>
