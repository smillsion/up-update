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
  await page.getByRole('link', { name: '设置' }).click()
  await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
  await expect(page.getByRole('button', { name: '退出' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-settings.png' })
})

test('pending delivery can be cancelled without layout overflow', async ({ page }) => {
  let deleteRequest = ''
  let deleteCSRF = ''
  await page.route('**/api/auth/me', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin', forcePasswordChange: false, csrfToken: 'csrf-test' }),
  }))
  await page.route('**/api/deliveries/**', route => {
    deleteRequest = `${route.request().method()} ${new URL(route.request().url()).pathname}`
    deleteCSRF = route.request().headers()['x-csrf-token'] || ''
    return route.fulfill({ status: 204 })
  })
  await page.route('**/api/deliveries', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify([{
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
    }]),
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
