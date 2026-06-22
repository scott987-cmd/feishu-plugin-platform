// 浏览器会话:加载/保存持久化登录态、截图留痕、验证码/风控守卫。
import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
import readline from 'node:readline';
import { cfg } from '../config.mjs';

// withPage 启动浏览器(默认无头、加载已保存的登录态),执行 fn,确保关闭。
export async function withPage(fn, { useState = true, headed = cfg.headed } = {}) {
  const browser = await chromium.launch({ headless: !headed, slowMo: cfg.slowMo });
  const hasState = useState && fs.existsSync(cfg.statePath);
  if (useState && !hasState) {
    await browser.close();
    throw new Error(`未找到登录态 ${cfg.statePath} —— 先运行 \`npm run login\` 抓一次会话`);
  }
  const ctx = await browser.newContext(hasState ? { storageState: cfg.statePath } : {});
  ctx.setDefaultTimeout(cfg.navTimeout);
  const page = await ctx.newPage();
  try {
    return await fn(page, ctx);
  } finally {
    await browser.close();
  }
}

// shot 全页截图到 screenshots/,返回路径。每个关键步骤都留痕,便于校准选择器。
export async function shot(page, name) {
  fs.mkdirSync(cfg.shotsDir, { recursive: true });
  const p = path.join(cfg.shotsDir, `${Date.now()}-${name}.png`);
  try { await page.screenshot({ path: p, fullPage: true }); } catch { /* ignore */ }
  return p;
}

// guardRisk 检测验证码/安全风控。命中则截图并抛错 —— 不尝试绕过(硬规则)。
export async function guardRisk(page, step) {
  const body = (await page.textContent('body').catch(() => '')) || '';
  if (/(安全验证|滑动验证|拖动滑块|请完成验证|verify you are human|captcha|风险控制|安全校验)/i.test(body)) {
    const s = await shot(page, `risk-${step}`);
    throw new Error(`步骤「${step}」遇到验证码/风控,RPA 无法继续(不绕过验证码)。请人工完成此步。截图:${s}`);
  }
}

// waitForEnter 在终端等待用户回车(用于一次性人工登录)。
export function waitForEnter(prompt) {
  return new Promise((resolve) => {
    const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
    rl.question(prompt, () => { rl.close(); resolve(); });
  });
}
