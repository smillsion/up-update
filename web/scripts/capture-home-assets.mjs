import { mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const baseURL=process.env.UP_UPDATE_CAPTURE_URL||'http://127.0.0.1:4173'
const outputDir=fileURLToPath(new URL('../public/home/',import.meta.url))
const now=Math.floor(Date.now()/1000)
const settings={
  bilibili:{configured:true,autoRefresh:true,status:'valid',name:'演示账号',lastValidated:now-180,error:''},
  bark:{configured:true,server:'https://api.day.app',level:'active',sound:'chime',quietEnabled:true,quietStart:'12:00',quietEnd:'14:00'},
}
const subscriptions=[
  {id:1,enabled:true,mid:'100001',name:'科技漫游',avatar:'/app-icon.png',latestBvid:'BV1demo01',latestTitle:'一周数码观察：真正值得关注的五件事',latestPublishedAt:now-3600,subscribedAt:now-86400,lastPolledAt:now-65,error:''},
  {id:2,enabled:true,mid:'100002',name:'电影手记',avatar:'/app-icon.png',latestBvid:'BV1demo02',latestTitle:'这个镜头为什么让人念念不忘',latestPublishedAt:now-7200,subscribedAt:now-72000,lastPolledAt:now-128,error:''},
  {id:3,enabled:true,mid:'100003',name:'旅行观察',avatar:'/app-icon.png',latestBvid:'BV1demo03',latestTitle:'在海边小城生活三天是什么体验',latestPublishedAt:now-86400,subscribedAt:now-36000,lastPolledAt:now-194,error:''},
  {id:4,enabled:false,mid:'100004',name:'独立游戏档案',avatar:'/app-icon.png',latestBvid:'BV1demo04',latestTitle:'本月值得加入愿望单的新作',latestPublishedAt:now-172800,subscribedAt:now-18000,lastPolledAt:now-260,error:''},
]

await mkdir(outputDir,{recursive:true})
const browser=await chromium.launch({channel:'chrome',headless:true})
const context=await browser.newContext({locale:'zh-CN',timezoneId:'Asia/Shanghai',colorScheme:'light',deviceScaleFactor:1})
const page=await context.newPage()
await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname
  if(path==='/api/auth/me')return route.fulfill({contentType:'application/json',body:JSON.stringify({id:1,username:'demo',displayName:'演示用户',role:'admin',forcePasswordChange:false,csrfToken:'capture-csrf'})})
  if(path==='/api/subscriptions')return route.fulfill({contentType:'application/json',body:JSON.stringify(subscriptions)})
  if(path==='/api/settings')return route.fulfill({contentType:'application/json',body:JSON.stringify(settings)})
  return route.fulfill({status:404,contentType:'application/json',body:'{"error":"not found"}'})
})

await page.setViewportSize({width:1440,height:900})
await page.goto(`${baseURL}/subscriptions`,{waitUntil:'networkidle'})
await page.getByRole('heading',{name:'UP 主订阅'}).waitFor()
await page.evaluate(()=>document.fonts.ready)
await page.screenshot({path:`${outputDir}/subscriptions-desktop.png`,fullPage:true,animations:'disabled'})

await page.setViewportSize({width:390,height:844})
await page.goto(`${baseURL}/settings`,{waitUntil:'networkidle'})
await page.getByRole('heading',{name:'设置'}).waitFor()
await page.evaluate(()=>document.fonts.ready)
await page.screenshot({path:`${outputDir}/settings-mobile.png`,animations:'disabled'})

await browser.close()
