const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const output = path.resolve(process.env.QA_OUTPUT || '../ui-test/screens');
fs.mkdirSync(output, { recursive: true });
let browser;
(async () => {
  browser = await chromium.launch({ headless: true, channel: 'msedge' });
  const results = [];
  for (const width of [320, 390, 900, 1440]) {
    for (const mode of ['normal', 'care-dark', 'care-light', 'xlarge']) {
      const ctx = await browser.newContext({ viewport: { width, height: 850 }, timezoneId: 'Asia/Shanghai' });
      await ctx.addInitScript(({ mode }) => {
        localStorage.setItem('nvr_demo_mode', '1');
        localStorage.setItem('nvr_token', 'demo');
        localStorage.setItem('nvr_user', JSON.stringify({ id: 'local', username: 'admin', role: 'admin' }));
        localStorage.setItem('nvr_settings_local', JSON.stringify({ theme: mode === 'care-light' ? 'light' : 'dark', fontSize: mode === 'xlarge' ? 'xlarge' : 'normal', careMode: mode.startsWith('care'), demoMode: true }));
      }, { mode });
      const page = await ctx.newPage();
      const errors = [];
      page.on('pageerror', e => errors.push(e.message));
      // Test the built-in demo without contacting a configured NVR or external media.
      await page.route(/^http:\/\/127\.0\.0\.1:5173\/api\//, route => route.fulfill({ status: 503, contentType: 'application/json', body: '{}' }));
      for (const route of ['settings', 'live', 'playback', 'events', 'server', 'login']) {
        await page.goto(`http://127.0.0.1:5173/#/${route}`);
        await page.locator('#app .page, #app .login-page').first().waitFor({ timeout: 15000 }).catch(() => {});
        await page.waitForTimeout(400);
        const state = await page.evaluate(() => {
          const visible = e => { const r=e.getBoundingClientRect(); return r.width && r.height && getComputedStyle(e).visibility !== 'hidden'; };
          const overflow = [...document.querySelectorAll('button,input,.van-cell,.van-tabbar,.sidebar,.font-opts,.controls')].filter(visible).filter(e=>{const r=e.getBoundingClientRect();return r.right>innerWidth+2||r.left< -2}).map(e=>({el:e.className,text:(e.textContent||'').trim().slice(0,35)}));
          const small = [...document.querySelectorAll('button,[role="switch"],.cal-nav,.day,.sp,.font-opt')].filter(visible).filter(e=>{const r=e.getBoundingClientRect();return r.width<44||r.height<44}).map(e=>({el:e.className,text:(e.textContent||'').trim().slice(0,20),w:Math.round(e.getBoundingClientRect().width),h:Math.round(e.getBoundingClientRect().height)}));
          return { overflow, small: small.slice(0,10), zoom:getComputedStyle(document.documentElement).zoom };
        });
        results.push({ width, mode, route, ...state, errors: [...errors] });
        if ((width === 390 && mode.startsWith('care')) || (width === 900 && mode==='xlarge') || (width === 1440 && route === 'settings')) await page.screenshot({ path: path.join(output, `${width}-${mode}-${route}.png`) });
      }
      await ctx.close();
    }
  }
  await browser.close();
  fs.writeFileSync(path.join(output, 'audit.json'), JSON.stringify(results, null, 2));
  console.log(JSON.stringify({ cases: results.length, overflow: results.filter(r=>r.overflow.length).map(r=>({width:r.width,mode:r.mode,route:r.route,overflow:r.overflow})), errors: [...new Set(results.flatMap(r=>r.errors))] }, null, 2));
  if (results.some(r => r.overflow.length || r.errors.length)) process.exitCode = 1;
})().catch(e => { console.error(e); process.exitCode=1; }).finally(async()=>{await browser?.close()});
