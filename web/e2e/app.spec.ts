import { expect, test, type Page } from '@playwright/test'

async function login(page: Page) {
  await page.goto('/login')
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('integration-test-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/subscriptions$/)
}

async function expectNoHorizontalOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }))
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.width)
}

test('desktop administration flow is usable', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await login(page)
  await expect(page.locator('.sidebar')).toBeVisible()
  await expect(page.locator('.mobile-nav')).toBeHidden()
  await page.getByRole('link', { name: '管理' }).click()
  await expect(page.getByRole('heading', { name: '管理' })).toBeVisible()
  await expect(page.locator('.user-info small').first()).toContainText('@admin')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/desktop-admin.png', fullPage: true })
})

test('mobile settings flow fits the viewport', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await login(page)
  await expect(page.locator('.sidebar')).toBeHidden()
  await expect(page.locator('.mobile-nav')).toBeVisible()
  await page.getByRole('link', { name: '设置', exact: true }).click()
  await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
  await expect(page.getByRole('button', { name: '退出' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '设置' })).toHaveCSS('user-select', 'none')
  await expect(page.getByLabel('Cookie')).toHaveCSS('user-select', 'text')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-settings.png' })
})

test('pending delivery can be cancelled without layout overflow', async ({ page }) => {
  let deleteRequest = ''
  let deleteCSRF = ''
  let deleted = false
  await page.route('**/api/auth/me', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin', forcePasswordChange: false, csrfToken: 'csrf-test' }),
  }))
  await page.route('**/api/deliveries/**', route => {
    deleteRequest = `${route.request().method()} ${new URL(route.request().url()).pathname}`
    deleteCSRF = route.request().headers()['x-csrf-token'] || ''
    deleted = true
    return route.fulfill({ status: 204 })
  })
  await page.route('**/api/deliveries?page=*', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({items:deleted?[]:[{
      id: 7,
      status: 'pending',
      attempts: 5,
      error: 'success',
      createdAt: 1_752_998_820,
      sentAt: null,
      bvid: 'BV1test',
      videoTitle: '可爱的人类幼崽',
      videoUrl: 'https://www.bilibili.com/video/BV1test',
      creatorName: '牢文luwen',
      creatorAvatar: '',
    }],page:1,pageSize:20,total:deleted?0:41,totalPages:deleted?0:3}),
  }))

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/history')
  await expect(page.getByRole('button', { name: '取消发送' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/desktop-pending-delivery.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-pending-delivery.png', fullPage: true })
  page.once('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: '取消发送' }).click()
  await expect(page.getByRole('heading', { name: '还没有通知' })).toBeVisible()
  expect(deleteRequest).toBe('DELETE /api/deliveries/7')
  expect(deleteCSRF).toBe('csrf-test')
})

test('following import dialog works on desktop and mobile', async ({ page }) => {
  let importRequest = ''
  await page.route('**/api/auth/me', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin', forcePasswordChange: false, csrfToken: 'csrf-test' }),
  }))
  await page.route('**/api/settings', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ bilibili: { configured: true, status: 'valid', name: 'tester', lastValidated: 1, error: '' }, bark: { configured: false, server: 'https://api.day.app', level: 'active', sound: '' } }),
  }))
  await page.route('**/api/subscriptions/import-followings', async route => {
    importRequest = route.request().postData() || ''
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ imported: 1, skipped: 0, initialized: 1, pending: 0 }) })
  })
  await page.route('**/api/subscriptions/followings?page=*', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ items: [{ mid: '2', name: 'New UP', avatar: '', subscribed: false }, { mid: '3', name: 'Subscribed UP', avatar: '', subscribed: true }], page: 1, pageSize: 50, total: 2, totalPages: 1 }),
  }))
  await page.route('**/api/subscriptions', route => route.fulfill({ contentType: 'application/json', body: '[]' }))

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/subscriptions')
  await expect(page.getByRole('heading', { name: 'UP 主订阅' })).toHaveCSS('user-select', 'none')
  await page.getByRole('button', { name: '从关注导入' }).click()
  await expect(page.getByRole('heading', { name: '从关注列表导入' })).toBeVisible()
  await expect(page.locator('.following-row input').first()).toHaveCSS('user-select', 'text')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/desktop-following-import.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-following-import.png', fullPage: true })
  await page.locator('.following-row input').check()
  await page.getByRole('button', { name: '导入选中 (1)' }).click()
  await expect(page.getByText('已导入 1 个订阅')).toBeVisible()
  expect(JSON.parse(importRequest)).toEqual({ page: 1, mids: ['2'] })
})
