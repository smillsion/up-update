import { expect, test, type Page } from '@playwright/test'

const user={id:1,username:'admin',displayName:'Admin',role:'admin',forcePasswordChange:false,csrfToken:'csrf-test'}
const schedule={timezone:'Asia/Shanghai',sleep:{start:'00:00',end:'08:00',intervalMinutes:120},work:{windows:[{start:'09:00',end:'12:00'},{start:'14:00',end:'18:00'}],intervalMinutes:15},free:{intervalMinutes:5}}

async function noOverflow(page:Page){
  const size=await page.evaluate(()=>({width:window.innerWidth,scrollWidth:document.documentElement.scrollWidth}))
  expect(size.scrollWidth).toBeLessThanOrEqual(size.width)
}

test.beforeEach(async({page})=>{
  await page.route('**/api/auth/me',route=>route.fulfill({contentType:'application/json',body:JSON.stringify(user)}))
})

test('subscription cards show the latest publication time and age',async({page})=>{
  const publishedAt=Math.floor(Date.now()/1000)-3*86400
  await page.route('**/api/settings',route=>route.fulfill({contentType:'application/json',body:JSON.stringify({bilibili:{configured:true,autoRefresh:true,status:'valid',name:'tester',lastValidated:1,error:''},bark:{configured:false,server:'https://api.day.app',level:'active',sound:'',quietEnabled:false,quietStart:'12:00',quietEnd:'14:00'}})}))
  await page.route('**/api/subscriptions',route=>route.fulfill({contentType:'application/json',body:JSON.stringify([{id:1,enabled:true,mid:'1',name:'Demo UP',avatar:'',latestBvid:'BV1',latestTitle:'Latest video',latestPublishedAt:publishedAt,subscribedAt:1,lastPolledAt:publishedAt,error:''}])}))
  await page.setViewportSize({width:390,height:844})
  await page.goto('/subscriptions')
  await expect(page.getByText(/投稿于 .* · 3天前/)).toBeVisible()
  await noOverflow(page)
  await page.screenshot({path:'test-results/mobile-publication-time.png',fullPage:true})
})

test('admin deletion requires the exact username on desktop and mobile',async({page})=>{
  let deleted=false,requestBody=''
  await page.route('**/api/admin/users/2',route=>{requestBody=route.request().postData()||'';deleted=true;return route.fulfill({status:204})})
  await page.route('**/api/admin/users',route=>route.fulfill({contentType:'application/json',body:JSON.stringify(deleted?[]:[{id:2,username:'member',displayName:'Member',role:'user',enabled:true,forcePasswordChange:false,createdAt:1,bilibiliStatus:'valid',barkConfigured:true,subscriptions:3}])}))
  await page.route('**/api/admin/system',route=>route.fulfill({contentType:'application/json',body:JSON.stringify({pollIntervalSeconds:300,pollSchedule:schedule,currentPeriod:'free',currentIntervalMinutes:5,nextTransitionAt:9999999999,activeCreators:1,activeUsers:2,pendingDeliveries:0})}))
  await page.setViewportSize({width:1440,height:900})
  await page.goto('/admin')
  await page.getByRole('button',{name:'永久删除用户'}).click()
  await expect(page.getByRole('dialog',{name:'永久删除 Member'})).toBeVisible()
  await expect(page.getByRole('button',{name:'永久删除',exact:true})).toBeDisabled()
  await page.getByLabel('输入用户名 member 确认').fill('member')
  await page.screenshot({path:'test-results/desktop-delete-user.png',fullPage:true})
  await page.setViewportSize({width:390,height:844})
  await noOverflow(page)
  await page.screenshot({path:'test-results/mobile-delete-user.png',fullPage:true})
  await page.getByRole('button',{name:'永久删除',exact:true}).click()
  await expect(page.getByText('用户及其数据已永久删除')).toBeVisible()
  expect(JSON.parse(requestBody)).toEqual({confirmUsername:'member'})
})

test('Bilibili logout explains and removes only local credentials',async({page})=>{
  let configured=true,method=''
  await page.route('**/api/settings/bilibili',route=>{method=route.request().method();configured=false;return route.fulfill({status:204})})
  await page.route('**/api/settings',route=>route.fulfill({contentType:'application/json',body:JSON.stringify({bilibili:{configured,autoRefresh:configured,status:configured?'valid':'missing',name:configured?'tester':'',lastValidated:null,error:''},bark:{configured:false,server:'https://api.day.app',level:'active',sound:'',quietEnabled:false,quietStart:'12:00',quietEnd:'14:00'}})}))
  await page.setViewportSize({width:390,height:844})
  await page.goto('/settings')
  await page.getByRole('button',{name:'退出 B 站账号'}).click()
  await expect(page.getByRole('dialog',{name:'退出 B 站账号'})).toContainText('不会退出手机或浏览器中的 B 站账号')
  await noOverflow(page)
  await page.screenshot({path:'test-results/mobile-bilibili-logout.png',fullPage:true})
  await page.getByRole('button',{name:'确认退出'}).click()
  await expect(page.getByText('已退出 B 站账号')).toBeVisible()
  expect(method).toBe('DELETE')
})
