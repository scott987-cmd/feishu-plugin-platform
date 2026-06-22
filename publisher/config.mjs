// 环境与路径配置。飞书/Lark 两套域名。
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.dirname(fileURLToPath(import.meta.url));

export const ENVS = {
  feishu: { console: 'https://open.feishu.cn', appList: 'https://open.feishu.cn/app' },
  lark: { console: 'https://open.larksuite.com', appList: 'https://open.larksuite.com/app' },
};

export const cfg = {
  env: process.env.OPDEV_ENV === 'lark' ? 'lark' : 'feishu',
  statePath: process.env.PUBLISHER_STATE || path.join(root, 'state.json'),
  shotsDir: path.join(root, 'screenshots'),
  headed: process.env.HEADED === '1',
  slowMo: Number(process.env.SLOWMO || 0),
  navTimeout: Number(process.env.NAV_TIMEOUT || 30000),
};

export const ENV = ENVS[cfg.env];
