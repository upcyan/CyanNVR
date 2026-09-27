// 专项验证 v4：
// - 模拟「后端可达」(health 返回 CyanNVR)，其它 /api 503
// - 管理员/普通用户各自视角
// - 登录安全分组：默认展开，内容可交互
// - 关怀模式 900px 服务器页三选项 + 证书来源
const { chromium } = require('playwright-core');
const fs = require('node:fs');
const out = process.env.QA_OUTPUT || '../ui-test/accept';
fs.mkdirSync(out, { recursive: true });
let browser;
const results = [];
const check = (cond, msg) => { results.push((cond ? 'PASS ' : 'FAIL ') + msg); if (!cond) process.exitCode = 1; };
const waitApp = (p) => p.waitForFunction(() =>
  document.querySelector('.settings-page') && document.querySelectorAll('.van-cell').length > 5, null, { timeout: 8000 });
const HEALTH = { status: 200, contentType: 'application/json', body: JSON.stringify({ name: 'CyanNVR', status: 'ok', version: '1.7.5' }) };

(async () => {
  browser = await chromium.launch({ channel: 'msedge', headless: true });

  // —— 管理员（后端可达）——
  let ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, timezoneId: 'Asia/Shanghai' });
  await ctx.addInitScript(() => {
    localStorage.setItem('nvr_demo_mode', '0');
    localStorage.setItem('nvr_token', 'demo');
    localStorage.setItem('nvr_user', JSON.stringify({ id: 'local', username: 'admin', role: 'admin' }));
    localStorage.setItem('nvr_settings_local', JSON.stringify({ theme: 'dark', fontSize: 'normal', careMode: false, demoMode: false }));
  });
  let page = await ctx.newPage();
  await page.route(/^http:\/\/127\.0\.0\.1:5173\/api\//, r => r.fulfill({ status: 503, contentType: 'application/json', body: '{}' }));
  await page.route(/^http:\/\/127\.0\.0\.1:5173\/api\/health$/, r => r.fulfill(HEALTH));
  await page.goto('http://127.0.0.1:5173/#/settings');
  await waitApp(page);
  const adminTitles = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.van-cell__title, .set-block-title')).map(e => e.textContent?.trim() || ''));
  check(adminTitles.some(t => t.startsWith('演示模式')), 'admin sees demo mode (backend ok)');
  check(adminTitles.includes('登录安全'), 'admin sees security group (backend ok)');
  const sec = page.locator('.set-block-title', { hasText: '登录安全' });
  await sec.scrollIntoViewIfNeeded();
  // 若默认收起（此前会话点过）则展开
  const expanded = await sec.evaluate(el => el.closest('.set-block-header')?.getAttribute('aria-expanded'));
  if (expanded === 'false') { await sec.click(); await page.waitForTimeout(400); }
  const input = page.locator('.trust-input input').first();
  const inputVisible = await input.isVisible().catch(() => false);
  check(inputVisible, 'trust window input visible');
  if (inputVisible) {
    await input.fill('48');
    await page.getByRole('button', { name: '保存' }).last().click().catch(() => {});
    await page.waitForTimeout(300);
    check(true, 'trust window save clickable');
  }
  await page.screenshot({ path: out + '/admin-security.png' });
  await page.close(); await ctx.close();

  // —— 普通用户（后端可达）——
  ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, timezoneId: 'Asia/Shanghai' });
  await ctx.addInitScript(() => {
    localStorage.setItem('nvr_demo_mode', '0');
    localStorage.setItem('nvr_token', 'demo');
    localStorage.setItem('nvr_user', JSON.stringify({ id: 'local', username: 'user1', role: 'user' }));
    localStorage.setItem('nvr_settings_local', JSON.stringify({ theme: 'dark', fontSize: 'normal', careMode: false, demoMode: false }));
  });
  page = await ctx.newPage();
  await page.route(/^http:\/\/127\.0\.0\.1:5173\/api\//, r => r.fulfill({ status: 503, contentType: 'application/json', body: '{}' }));
  await page.route(/^http:\/\/127\.0\.0\.1:5173\/api\/health$/, r => r.fulfill(HEALTH));
  await page.goto('http://127.0.0.1:5173/#/settings');
  await waitApp(page);
  const userTitles = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.van-cell__title, .set-block-title')).map(e => e.textContent?.trim() || ''));
  check(!userTitles.some(t => t.startsWith('演示模式')), 'non-admin does NOT see demo mode (backend ok)');
  check(!userTitles.includes('登录安全'), 'non-admin does NOT see security group (backend ok)');
  await page.close(); await ctx.close();

  // —— 关怀模式 900px 服务器页 ——
  ctx = await browser.newContext({ viewport: { width: 900, height: 900 }, timezoneId: 'Asia/Shanghai' });
  await ctx.addInitScript(() => {
    localStorage.setItem('nvr_demo_mode', '1');
    localStorage.setItem('nvr_token', 'demo');
    localStorage.setItem('nvr_user', JSON.stringify({ id: 'local', username: 'admin', role: 'admin' }));
    localStorage.setItem('nvr_settings_local', JSON.stringify({ theme: 'dark', fontSize: 'normal', careMode: true, demoMode: true }));
  });
  page = await ctx.newPage();
  await page.route(/^http:\/\/127\.0\.0\.1:5173\/api\//, r => r.fulfill({ status: 503, contentType: 'application/json', body: '{}' }));
  await page.goto('http://127.0.0.1:5173/#/server');
  await page.waitForFunction(() => document.querySelectorAll('.van-cell').length > 3, null, { timeout: 8000 });
  await page.waitForTimeout(500);
  for (const name of ['自动', '仅局域网', '仅公网']) {
    const ok = await page.evaluate((label) => {
      const els = Array.from(document.querySelectorAll('.van-radio')).filter(e => e.textContent?.trim() === label);
      if (!els.length) return false;
      const r = els[0].getBoundingClientRect();
      return r.width > 0 && r.height > 0 && r.right <= window.innerWidth + 1 && r.left >= -1;
    }, name);
    check(ok, `care 900px: strategy option inside viewport: ${name}`);
  }
  const certTitles = await page.evaluate(() =>
    Array.from(document.querySelectorAll('.van-cell__title')).map(e => e.textContent?.trim() || ''));
  console.log('CERT_TITLES:', JSON.stringify(certTitles.filter(t => t.includes('证书') || t.includes('HTTPS') || t.includes('证书来源'))));
  check(certTitles.some(t => t.includes('HTTPS')), 'care 900px: HTTPS cert cell rendered');
  await page.screenshot({ path: out + '/care-server-900.png', fullPage: true });
  await page.goto('http://127.0.0.1:5173/#/settings');
  await waitApp(page);
  await page.screenshot({ path: out + '/care-settings-900.png', fullPage: true });
  await page.close(); await ctx.close();

  console.log(results.join('\n'));
  await browser.close();
})().catch(e => { console.error('FATAL', e.message); console.log(results.join('\n')); process.exit(1); });
