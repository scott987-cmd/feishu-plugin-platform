// 飞书开发者后台驱动 —— 模拟点击跑无 OpenAPI 的步骤。
// 入口选择器已对真实后台逐个实测校准(2026-06-22):
//   create-app 入口/字段填充、register-ext 两分支(已存在直读 + 添加分支三处歧义已修)、
//   publish 版本页入口,均按实测文案 + 唯一性/作用域校准。
// 仍标 [CALIBRATE] 的是「改租户的提交动作之后」未实测确认的环节:create-app 最终提交不落地(需 HEADED
//   补点选图标等)、建版本表单内字段(异步加载)。首跑务必 HEADED=1 看 screenshots/。
import { chromium } from 'playwright';
import { cfg, ENV } from '../config.mjs';
import { withPage, shot, guardRisk } from './session.mjs';

// 标签在上方 / 无 placeholder 时:填「标签文本之后的第一个文本输入框」。
// 注意:input 限定 type=text(或无 type),跳过中间的隐藏 radio/file(如图标颜色单选)。
async function fillAfter(page, labelText, value, kind = 'input') {
  const node = kind === 'textarea' ? 'textarea' : 'input[not(@type) or @type="text"]';
  const loc = page.locator(`xpath=//*[contains(normalize-space(text()),"${labelText}")]/following::${node}[1]`).first();
  await loc.waitFor({ state: 'visible', timeout: cfg.navTimeout });
  await loc.fill(value);
}
async function clickText(page, text) {
  const b = page.getByRole('button', { name: text, exact: false }).first();
  if (await b.count()) { await b.click(); return; }
  await page.getByText(text, { exact: false }).first().click();
}
async function clickExact(page, text) {
  const b = page.getByRole('button', { name: text, exact: true }).first();
  if (await b.count()) { await b.click(); return; }
  await page.getByText(text, { exact: true }).first().click();
}

// ① 一次性:有头浏览器人工登录;轮询检测进入应用列表后自动保存登录态(不依赖 stdin)。
export async function captureLogin({ timeoutMs = Number(process.env.LOGIN_TIMEOUT || 240000) } = {}) {
  const browser = await chromium.launch({ headless: false });
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto(ENV.appList, { waitUntil: 'domcontentloaded' });
  console.log(`已弹出浏览器,请完成飞书登录(扫码/账号)。检测到进入应用列表后自动保存(最多等 ${Math.round(timeoutMs / 1000)}s)…`);
  const host = new URL(ENV.appList).host;
  const ok = await page
    .waitForFunction(
      (h) => location.host === h && !/passport|login|sso/i.test(location.pathname) &&
        !!document.body && document.body.innerText.replace(/\s/g, '').includes('创建'),
      host,
      { timeout: timeoutMs, polling: 1500 },
    )
    .then(() => true)
    .catch(() => false);
  await shot(page, ok ? 'login-ok' : 'login-timeout');
  if (!ok) { await browser.close(); throw new Error('未检测到登录完成(超时)。重试或调大 LOGIN_TIMEOUT。'); }
  await ctx.storageState({ path: cfg.statePath });
  console.log('✓ 登录态已保存 →', cfg.statePath);
  await browser.close();
}

// ② 创建企业自建应用,返回 appId。(弹窗:应用名称*/应用描述*/应用图标*[有默认], 底部 取消/创建)
export async function createApp({ name, desc = '由 publisher 自动创建' }) {
  return withPage(async (page) => {
    await page.goto(ENV.appList, { waitUntil: 'domcontentloaded' });
    await guardRisk(page, 'app-list');
    await shot(page, 'app-list');
    await clickText(page, '创建企业自建应用');
    await page.waitForTimeout(800);
    await fillAfter(page, '应用名称', name, 'input');
    await fillAfter(page, '应用描述', desc, 'textarea').catch(() => {});
    await shot(page, 'app-create-dialog');
    // 提交:页脚「创建」按钮(exact 文本=创建,实测就是它)。
    // [CALIBRATE] 已实测:名称/描述能填对、能点中此按钮;但仅填名称+描述提交「未建出应用」,
    //   弹窗下方很可能还有必填项(应用图标需显式点选 / 或更多字段)。需 HEADED=1 观察弹窗、
    //   补上点选图标等步骤。另:创建成功后重定向到「按名过滤的应用列表」,而非 /app/cli_xxx。
    await page.getByText('创建', { exact: true }).first().click();
    await page.waitForLoadState('networkidle').catch(() => {});
    await page.waitForTimeout(2500);
    await shot(page, 'app-after-create');
    // 成功后从列表里按名取新应用的 cli_ id。
    await page.goto(ENV.appList, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(2000);
    const card = page.locator(`a[href*="/app/cli_"]:has-text("${name}")`).first();
    if (!(await card.count())) {
      throw new Error(`未在列表找到新建应用「${name}」——提交未生效(多半弹窗下方有未完成的必填项,如图标)。请 HEADED=1 观察 screenshots/ 后补点选。`);
    }
    const m = ((await card.getAttribute('href')) || '').match(/cli_[A-Za-z0-9]+/);
    if (!m) throw new Error('找到卡片但未解析 appId');
    await shot(page, 'app-created');
    return m[0];
  });
}

// ③ 登记/读取「数据表视图」扩展并抓 blockTypeID。已存在则直接读(幂等),否则走添加流程。
export async function registerTableViewExtension({ appId, name = '渲染器' }) {
  return withPage(async (page) => {
    await page.goto(`${ENV.console}/app/${appId}/baseinfo`, { waitUntil: 'domcontentloaded' });
    await guardRisk(page, 'app');
    await page.waitForTimeout(1500);
    const existing = page.getByText('多维表格数据表视图', { exact: false }).first();
    if (await existing.count()) {
      await existing.click().catch(() => {});            // 左菜单已有该扩展 → 进详情读 blk_
      await page.waitForLoadState('networkidle').catch(() => {});
      await page.waitForTimeout(2000);
    } else {
      // 添加分支(扩展不存在时)。入口已实测校准(2026-06-22),三处歧义已修:
      //   ① 「多维表格插件」必须 exact —— 否则会命中应用名「多维表格插件平台」(整页 count=2)。
      //   ② 「数据表视图」必须限定在 dialog 作用域 —— 整页 count=2,弹窗内 count=1。
      //   ③ 「添加」必须限定在 dialog 作用域 —— 整页有 9 个「添加」按钮,弹窗内唯一(页脚提交)。
      await clickText(page, '添加应用能力');                 // 入口(count=1)
      await page.waitForTimeout(2000);
      await page.getByText('多维表格插件', { exact: true }).first().click(); // 能力卡片(exact)
      await page.waitForTimeout(2000);
      const dlg = page.getByRole('dialog');                 // 类型弹窗作用域(实测 count=1)
      await dlg.getByText('数据表视图', { exact: true }).first().click();    // 选类型(作用域内唯一)
      await shot(page, 'ext-type');
      await dlg.getByRole('button', { name: '添加', exact: false }).click(); // 页脚「添加」提交(作用域内唯一)
      await page.waitForLoadState('networkidle').catch(() => {});
      await page.waitForTimeout(2500);
      if (name) await fillAfter(page, '小组件名称', name).catch(() => {});
    }
    await shot(page, 'ext-page');
    const body = (await page.textContent('body')) || '';
    const m = body.match(/blk_[a-z0-9]+/i);
    if (!m) throw new Error('未找到 blockTypeID(blk_...),见截图,DOM 可能变动需校准');
    return m[0];
  });
}

// ④ 创建应用版本 + 申请线上发布。版本页入口已实测:直连 /app/{id}/version(实测 URL 对);
//    不点左菜单「版本管理与发布」——其文案整页 count=2 有歧义,直连 URL 更稳。
export async function createVersionAndPublish({ appId, version, notes = 'release via publisher' }) {
  return withPage(async (page) => {
    await page.goto(`${ENV.console}/app/${appId}/version`, { waitUntil: 'domcontentloaded' });
    await guardRisk(page, 'version');
    await page.waitForTimeout(1500);
    await shot(page, 'version-page');
    await clickText(page, '创建版本');
    await page.waitForLoadState('networkidle').catch(() => {});
    await page.waitForTimeout(3500); // 创建版本是异步加载的独立页,留足时间
    await shot(page, 'version-form-loaded');
    // [CALIBRATE] 表单字段标签以真实页为准(常见:应用版本号 / 更新说明 / 可用范围 / 权限);异步加载后才出现
    if (version) await fillAfter(page, '应用版本号', version).catch(() => {});
    await fillAfter(page, '更新说明', notes, 'textarea').catch(() => {});
    await shot(page, 'version-form');
    await clickText(page, '申请线上发布'); // [CALIBRATE] 提交按钮文案
    await page.waitForTimeout(2000);
    await guardRisk(page, 'after-submit');
    await shot(page, 'version-submitted');
    return { appId, version, submitted: true };
  });
}
