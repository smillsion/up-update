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

test('public homepage routes visitors and authenticated users correctly', async ({ page }) => {
  await page.route('**/api/auth/me', route => route.fulfill({
    status: 401,
    contentType: 'application/json',
    body: '{"error":"未登录"}',
  }))
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'up-update', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '登录系统' }).first()).toHaveAttribute('href', '/login')
  await expectNoHorizontalOverflow(page)
  const featuresTop = await page.locator('#features').evaluate(element => element.getBoundingClientRect().top)
  expect(featuresTop).toBeLessThan(900)
  await page.screenshot({ path: 'test-results/home-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.reload()
  await expectNoHorizontalOverflow(page)
  await expect(page.getByRole('heading', { name: 'up-update', exact: true })).toBeVisible()
  await page.locator('#preview').scrollIntoViewIfNeeded()
  await expect.poll(() => page.locator('.home-product-shots img').evaluateAll(images => images.every(image => (image as HTMLImageElement).complete && (image as HTMLImageElement).naturalWidth > 0))).toBe(true)
  await page.screenshot({ path: 'test-results/home-mobile.png', fullPage: true })

  await page.setViewportSize({ width: 320, height: 700 })
  await page.reload()
  await expectNoHorizontalOverflow(page)
  const mobileFeaturesTop = await page.locator('#features').evaluate(element => element.getBoundingClientRect().top)
  expect(mobileFeaturesTop).toBeLessThan(700)
})

test('authenticated users enter subscriptions from root and can open the homepage', async ({ page }) => {
  await page.route('**/api/auth/me', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ id: 1, username: 'demo', displayName: '演示用户', role: 'user', forcePasswordChange: false, csrfToken: 'csrf-test' }),
  }))
  await page.route('**/api/subscriptions', route => route.fulfill({ contentType: 'application/json', body: '[]' }))
  await page.route('**/api/settings', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ bilibili: { configured: false, autoRefresh: false, status: 'missing', name: '', lastValidated: null, error: '' }, bark: { configured: false, server: 'https://api.day.app', level: 'active', sound: '', quietEnabled: false, quietStart: '12:00', quietEnd: '14:00' } }),
  }))
  await page.goto('/')
  await expect(page).toHaveURL(/\/subscriptions$/)
  await page.getByRole('link', { name: '主页', exact: true }).click()
  await expect(page).toHaveURL(/\/about$/)
  await expect(page.getByRole('link', { name: '进入控制台' }).first()).toHaveAttribute('href', '/subscriptions')
})

test('desktop administration flow is usable', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await login(page)
  await expect(page.locator('.sidebar')).toBeVisible()
  await expect(page.locator('.mobile-nav')).toBeHidden()
  await page.getByRole('link', { name: '管理' }).click()
  await expect(page.getByRole('heading', { name: '管理' })).toBeVisible()
  await expect(page.locator('.alert.error')).toHaveCount(0)
  await expect(page.getByLabel('间隔（分钟）').first()).toHaveValue('120')
  await expect(page.locator('.user-info small').first()).toContainText('@admin')
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/desktop-admin.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-admin.png', fullPage: true })
  await page.locator('.main-content').evaluate(element => { element.scrollTop = element.scrollHeight })
  await page.screenshot({ path: 'test-results/mobile-admin-bottom.png' })
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

  await page.setViewportSize({ width: 320, height: 700 })
  await expectNoHorizontalOverflow(page)
  await expect(page.locator('.mobile-nav')).toBeVisible()
  const navItemsFit = await page.locator('.mobile-nav').evaluate(nav => Array.from(nav.children).every(item => {
    const box = item.getBoundingClientRect()
    return box.left >= 0 && box.right <= window.innerWidth
  }))
  expect(navItemsFit).toBe(true)
  await page.screenshot({ path: 'test-results/mobile-settings-320.png' })
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
    body: JSON.stringify({ bilibili: { configured: true, autoRefresh: true, status: 'valid', name: 'tester', lastValidated: 1, error: '' }, bark: { configured: false, server: 'https://api.day.app', level: 'active', sound: '', quietEnabled: false, quietStart: '12:00', quietEnd: '14:00' } }),
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

test('Bilibili QR login renders a scannable dialog on desktop and mobile', async ({ page }) => {
  await page.route('**/api/auth/me', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin', forcePasswordChange: false, csrfToken: 'csrf-test' }),
  }))
  await page.route('**/api/settings', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ bilibili: { configured: false, autoRefresh: false, status: 'missing', name: '', lastValidated: null, error: '' }, bark: { configured: false, server: 'https://api.day.app', level: 'active', sound: '', quietEnabled: false, quietStart: '12:00', quietEnd: '14:00' } }),
  }))
  await page.route('**/api/settings/bilibili/qrcode**', route => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 })
    if (path.endsWith('/poll')) return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ status: 'waiting', message: '请使用哔哩哔哩客户端扫码' }) })
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ sessionId: 'qr-test', qrUrl: 'https://passport.bilibili.com/scan?qrcode_key=test', expiresAt: 9_999_999_999 }) })
  })

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/settings')
  await page.getByRole('button', { name: '扫码登录' }).click()
  await expect(page.getByRole('dialog', { name: '扫码登录' })).toBeVisible()
  const darkPixels = await page.getByRole('img', { name: 'B 站登录二维码' }).evaluate((canvas: HTMLCanvasElement) => {
    const pixels = canvas.getContext('2d')!.getImageData(0, 0, canvas.width, canvas.height).data
    let count = 0
    for (let index = 0; index < pixels.length; index += 4) if (pixels[index] < 80 && pixels[index + 1] < 80 && pixels[index + 2] < 80 && pixels[index + 3] > 0) count++
    return count
  })
  expect(darkPixels).toBeGreaterThan(1000)
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/desktop-bilibili-qr.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('dialog', { name: '扫码登录' })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-bilibili-qr.png', fullPage: true })
})

test('Bark draft settings can be tested and saved without re-entering the key', async ({ page }) => {
  let testBody: Record<string, unknown> | null = null
  let saveBody: Record<string, unknown> | null = null
  await page.route('**/api/auth/me', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin', forcePasswordChange: false, csrfToken: 'csrf-test' }),
  }))
  await page.route('**/api/settings', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ bilibili: { configured: true, autoRefresh: true, status: 'valid', name: 'tester', lastValidated: 1, error: '' }, bark: { configured: true, server: 'https://bark.example', level: 'active', sound: '', quietEnabled: false, quietStart: '12:00', quietEnd: '14:00' } }),
  }))
  await page.route('**/api/settings/bark/test', route => {
    testBody = JSON.parse(route.request().postData() || '{}')
    return route.fulfill({ contentType: 'application/json', body: '{"ok":true}' })
  })
  await page.route('**/api/settings/bark', route => {
    saveBody = JSON.parse(route.request().postData() || '{}')
    return route.fulfill({ contentType: 'application/json', body: '{"ok":true}' })
  })

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/settings')
  await expect(page.getByLabel('Device Key')).not.toHaveAttribute('required', '')
  await page.getByLabel('通知级别').selectOption('critical')
  await page.getByLabel('提示音').selectOption('alarm')
  await page.getByRole('button', { name: '发送测试' }).click()
  await expect(page.getByText('已按当前表单发送测试通知')).toBeVisible()
  await expect(page.getByText('午休延迟补发')).toBeVisible()
  expect(testBody).toMatchObject({ deviceKey: '', level: 'critical', sound: 'alarm' })
  await page.locator('.quiet-settings input[type="checkbox"]').check()
  await page.getByRole('button', { name: '保存 Bark' }).click()
  expect(saveBody).toMatchObject({ deviceKey: '', level: 'critical', sound: 'alarm', quietEnabled: true, quietStart: '12:00', quietEnd: '14:00' })
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/desktop-bark-settings.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoHorizontalOverflow(page)
  await page.screenshot({ path: 'test-results/mobile-bark-settings.png' })
  await page.locator('.main-content').evaluate(element => { element.scrollTop = element.scrollHeight })
  const saveBox = await page.getByRole('button', { name: '保存 Bark' }).boundingBox()
  const navBox = await page.locator('.mobile-nav').boundingBox()
  expect(saveBox && navBox && saveBox.y + saveBox.height <= navBox.y).toBeTruthy()
  await page.screenshot({ path: 'test-results/mobile-bark-settings-bottom.png' })
})
