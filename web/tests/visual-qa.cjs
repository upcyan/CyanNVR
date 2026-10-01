// 本轮 UI 改版的视觉状态自查（只读 DOM/CSS，不改任何页面状态）。
// 用法：先启动 dev 服务器，然后
//   PLAYWRIGHT_MODULE=<path> QA_OUTPUT=<dir> node tests/visual-qa.cjs
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const output = path.resolve(process.env.QA_OUTPUT || '../ui-test/visual');
fs.mkdirSync(output, { recursive: true });
let browser;
let failed = 0;
function check(name, ok, detail) {
  console.log((ok ? 'PASS' : 'FAIL') + ' ' + name + (detail ? ' :: ' + detail : ''));
  if (!ok) failed++;
}
(async () => {
  browser = await chromium.launch({ headless: true, channel: 'msedge' });
  for (const mode of ['normal', 'care-light']) {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 850 }, timezoneId: 'Asia/Shanghai' });
    await ctx.addInitScript(({ mode }) => {
      localStorage.setItem('nvr_demo_mode', '1');
      localStorage.setItem('nvr_token', 'demo');
      localStorage.setItem('nvr_user', JSON.stringify({ id: 'local', username: 'admin', role: 'admin' }));
      localStorage.setItem('nvr_settings_local', JSON.stringify({
        theme: mode === 'care-light' ? 'light' : 'dark',
        fontSize: 'normal',
        careMode: mode === 'care-light',
        demoMode: true,
      }));
    }, { mode });
    const page = await ctx.newPage();
    const errors = [];
    page.on('pageerror', (e) => errors.push(e.message));

    // ── Live 页：在线率条、强调线、图标按钮、录制点 ──
    await page.goto('http://127.0.0.1:5173/#/live');
    await page.locator('#app .page').first().waitFor({ timeout: 15000 });
    await page.waitForTimeout(500);
    const live = await page.evaluate(() => {
      const bar = document.querySelector('.side-status .status-bar');
      const barFill = bar && bar.querySelector('i');
      const line = getComputedStyle(document.querySelector('.hd-title'), '::after');
      const btn = document.querySelector('.icon-btn');
      const dot = document.querySelector('.cam-icon.recording');
      const vis = (el) => { if (!el) return false; const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
      return {
        barVisible: vis(bar),
        barFillWidth: barFill ? barFill.style.width : '',
        lineBg: line.backgroundImage,
        btnBorder: btn ? getComputedStyle(btn).borderTopWidth : '',
        recDot: vis(dot),
        bodyBg: getComputedStyle(document.body).backgroundColor,
      };
    });
    check(`[${mode}] 在线率条可见且有填充`, live.barVisible && /%$/.test(live.barFillWidth), `width=${live.barFillWidth}`);
    check(`[${mode}] Live 标题强调线是品牌渐变`, /linear-gradient/.test(live.lineBg), live.lineBg.slice(0, 60));
    check(`[${mode}] 图标按钮有描边`, live.btnBorder !== '0px', live.btnBorder);
    check(`[${mode}] 录制状态点渲染`, live.recDot);

    // ── 登录页：氛围层与渐变标题 ──
    await page.goto('http://127.0.5173'.replace('0.5173', '0.1:5173') + '/#/login');
    await page.locator('#app .login-page, #app .page.login').first().waitFor({ timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(400);
    const login = await page.evaluate(() => {
      const pageEl = document.querySelector('.login');
      const before = pageEl ? getComputedStyle(pageEl, '::before') : null;
      const h1 = document.querySelector('.brand h1');
      const logo = document.querySelector('.login .logo');
      return {
        bg: before ? before.backgroundImage : '',
        h1Clip: h1 ? getComputedStyle(h1).webkitBackgroundClip || getComputedStyle(h1).backgroundClip : '',
        logoFilter: logo ? getComputedStyle(logo).filter : '',
      };
    });
    check(`[${mode}] 登录页氛围层有渐变`, /radial-gradient/.test(login.bg), login.bg.slice(0, 60));
    check(`[${mode}] 品牌标题渐变文字`, /text/.test(login.h1Clip), login.h1Clip);
    check(`[${mode}] logo 光晕`, /drop-shadow/.test(login.logoFilter), login.logoFilter.slice(0, 50));

    // ── 设置页：分组标题强调短杠 ──
    await page.goto('http://127.0.0.1:5173/#/settings');
    await page.locator('#app .page').first().waitFor({ timeout: 15000 });
    await page.waitForTimeout(500);
    const setTitle = await page.evaluate(() => {
      const el = document.querySelector('.set-block-title');
      if (!el) return { found: false };
      const st = getComputedStyle(el, '::before');
      const r = el.getBoundingClientRect();
      return { found: true, w: st.width, bg: st.backgroundImage, color: getComputedStyle(el).color, titleW: Math.round(r.width) };
    });
    check(`[${mode}] 设置分组标题短杠`, setTitle.found && /linear-gradient/.test(setTitle.bg) && setTitle.w === '3px',
      setTitle.found ? setTitle.bg.slice(0, 50) : 'no .set-block-title');
    check(`[${mode}] 分组标题用主文字色`, !setTitle.found || setTitle.color === 'rgb(31, 39, 48)' || setTitle.color === 'rgb(230, 233, 239)' || setTitle.color === 'rgb(11, 18, 32)',
      setTitle.color);

    // ── Vant 主色映射：设置页主按钮/开关 ──
    const vant = await page.evaluate(() => {
      const btn = document.querySelector('.save-area .van-button--primary');
      const switchEl = document.querySelector('.van-switch');
      const b = btn ? getComputedStyle(btn).backgroundColor : '';
      const s = switchEl ? getComputedStyle(switchEl).backgroundColor : '';
      return { b, s };
    });
    check(`[${mode}] Vant 主按钮已映射品牌色`, /rgb\(46, 168, 255\)|rgb\(11, 98, 196\)/.test(vant.b), vant.b);
    check(`[${mode}] 无页面异常`, errors.length === 0, errors.join('; ').slice(0, 120));

    await page.screenshot({ path: path.join(output, `visual-${mode}.png`), fullPage: false });
    await ctx.close();
  }
  await browser.close();
  console.log(failed ? `\n${failed} 项未通过` : '\n全部通过');
  if (failed) process.exitCode = 1;
})().catch((e) => { console.error(e); process.exitCode = 1; }).finally(async () => { await browser?.close() });
