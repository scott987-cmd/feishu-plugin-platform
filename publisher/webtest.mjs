// 浏览器侧验证(自主):①真实跨域 fetch 测后端 CORS 预检+鉴权+TLS;②加载真插件 bundle 看启动。
import { chromium } from 'playwright';
const B = 'https://your-host.example.com';
const T = process.env.TOKEN || '';
const PORT = process.env.PORT || '8799';
const browser = await chromium.launch();
const page = await browser.newPage();
const logs = [], net = [];
page.on('console', (m) => logs.push(`[${m.type()}] ${m.text()}`.slice(0, 180)));
page.on('pageerror', (e) => logs.push(`[pageerror] ${e.message}`.slice(0, 180)));
page.on('requestfinished', async (r) => {
  if (r.url().includes('sslip.io')) { const rs = await r.response(); net.push(`${r.method()} ${r.url().replace(B, '')} -> ${rs ? rs.status() : '?'}`); }
});
page.on('requestfailed', (r) => { if (r.url().includes('sslip.io')) net.push(`FAILED ${r.url().replace(B, '')}: ${r.failure()?.errorText}`); });

// ① 从真实 origin(localhost)发起插件那个带 Bearer 的跨域 GET
await page.goto(`http://localhost:${PORT}/`, { waitUntil: 'domcontentloaded' }).catch((e) => logs.push('goto-blank: ' + e.message));
const direct = await page.evaluate(async ({ B, T }) => {
  const o = {};
  try {
    const r = await fetch(B + '/api/apps', { headers: { Authorization: 'Bearer ' + T, Accept: 'application/json' } });
    o.status = r.status; o.acao = r.headers.get('access-control-allow-origin');
    const j = await r.json(); o.count = Array.isArray(j) ? j.length : null; o.first = Array.isArray(j) && j[0] ? j[0].name : null;
  } catch (e) { o.error = String(e); }
  return o;
}, { B, T });
console.log('DIRECT_FETCH', JSON.stringify(direct));

// ② 加载真插件 bundle,看启动到哪、是否去拉后端、报什么错
logs.length = 0; net.length = 0;
await page.goto(`http://localhost:${PORT}/index.html`, { waitUntil: 'load', timeout: 30000 }).catch((e) => logs.push('goto-bundle: ' + e.message));
await page.waitForTimeout(5000);
console.log('BUNDLE_NET', JSON.stringify(net));
console.log('BUNDLE_BODY', ((await page.textContent('body').catch(() => '')) || '').replace(/\s+/g, ' ').slice(0, 200));
console.log('BUNDLE_LOGS:');
logs.slice(-22).forEach((l) => console.log('  ' + l));
await page.screenshot({ path: '/tmp/plugin-standalone.png', fullPage: true });
await browser.close();
console.log('DONE');
