// publisher CLI:login | create-app | register-ext | publish
import { cfg } from '../config.mjs';

function parseFlags(argv) {
  const f = {};
  for (let i = 0; i < argv.length; i++) {
    if (argv[i].startsWith('--')) { f[argv[i].slice(2)] = argv[i + 1] && !argv[i + 1].startsWith('--') ? argv[++i] : true; }
  }
  return f;
}

const USAGE = `publisher — 用模拟操作驱动飞书开发者后台(无 API 的步骤)
环境: OPDEV_ENV=feishu|lark  HEADED=1(有头,首跑建议)  SLOWMO=300

用法:
  node src/cli.mjs login                                  一次性人工登录,保存 state.json
  node src/cli.mjs create-app --name "我的应用" [--desc ..]  创建自建应用 → 打印 appId
  node src/cli.mjs register-ext --app cli_xxx [--name 渲染器]  登记数据表视图扩展 → 打印 blockTypeID
  node src/cli.mjs publish --app cli_xxx --version 0.1.1 [--notes ..]  创建版本 + 申请线上发布
`;

const cmd = process.argv[2];
const flags = parseFlags(process.argv.slice(3));

if (!cmd || !['login', 'create-app', 'register-ext', 'publish'].includes(cmd)) {
  console.log(USAGE);
  process.exit(cmd ? 1 : 0);
}

try {
  // 懒加载:console.mjs 依赖 playwright,help 路径不应要求先装依赖。
  const C = await import('./console.mjs');
  if (cmd === 'login') {
    await C.captureLogin();
  } else if (cmd === 'create-app') {
    if (!flags.name) throw new Error('需要 --name');
    console.log('APP_ID=' + (await C.createApp({ name: flags.name, desc: flags.desc })));
  } else if (cmd === 'register-ext') {
    if (!flags.app) throw new Error('需要 --app cli_xxx');
    console.log('BLOCK_TYPE_ID=' + (await C.registerTableViewExtension({ appId: flags.app, name: flags.name })));
  } else if (cmd === 'publish') {
    if (!flags.app || !flags.version) throw new Error('需要 --app 和 --version');
    console.log('SUBMITTED ' + JSON.stringify(await C.createVersionAndPublish({ appId: flags.app, version: flags.version, notes: flags.notes })));
  }
  console.log('(env=' + cfg.env + ', headed=' + cfg.headed + ', state=' + cfg.statePath + ')');
} catch (e) {
  console.error('✗ ' + (e && e.message ? e.message : e));
  console.error('  提示:首跑用 HEADED=1 观察、看 screenshots/ 校准 [CALIBRATE] 选择器。');
  process.exit(1);
}
