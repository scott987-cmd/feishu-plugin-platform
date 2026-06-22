// filter.ts 单测:用 esbuild 把 TS 打包成 ESM 在 node 跑(filter 只依赖纯 cellValue,不引 SDK)。
import { build } from 'esbuild';

const out = await build({
  entryPoints: ['src/filter.ts'],
  bundle: true, format: 'esm', write: false, platform: 'node', logLevel: 'silent',
});
const mod = await import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));
const { applyFilter, __setNowForTest } = mod;
__setNowForTest(() => new Date('2026-06-15T00:00:00Z')); // 锚定:本月=6,本年=2026

let pass = 0, fail = 0;
const eq = (name, got, want) => {
  const ok = got === want;
  console.log(`${ok ? '✓' : '✗'} ${name} => ${got}${ok ? '' : ` (want ${want})`}`);
  ok ? pass++ : fail++;
};

const recs = [
  { 订单状态: '已完成', 金额: 598, 下单时间: new Date('2026-06-10').getTime(), 商品名称: '无线蓝牙耳机' },
  { 订单状态: '已付款', 金额: 1299, 下单时间: new Date('2026-06-20').getTime(), 商品名称: '智能手表' },
  { 订单状态: '待付款', 金额: 599, 下单时间: new Date('2026-05-01').getTime(), 商品名称: '运动鞋' },
];
const n = (f) => applyFilter(recs, f).records.length;

eq('=(单选)', n('订单状态=已完成'), 1);
eq('!=', n('订单状态!=已完成'), 2);
eq('数字 >=', n('金额>=599'), 2);
eq('数字 >', n('金额>600'), 1);
eq('month()=THIS_MONTH', n('month(下单时间)=THIS_MONTH'), 2);
eq('year()=THIS_YEAR', n('year(下单时间)=THIS_YEAR'), 3);
eq('in [列表]', n('订单状态 in [已完成,已付款]'), 2);
eq('contains', n('商品名称 contains 手'), 1);
eq('AND', n('金额>500 AND 订单状态=已完成'), 1);
eq('OR', n('订单状态=已完成 OR 订单状态=待付款'), 2);

// 空 filter:不过滤
const e = applyFilter(recs, '');
eq('空-applied', e.applied, false);
eq('空-count', e.records.length, 3);

// 语法非法:error=true,不过滤,不抛
const bad = applyFilter(recs, '金额 ~~ 5');
eq('非法-error', bad.error, true);
eq('非法-applied', bad.applied, false);
eq('非法-count', bad.records.length, 3);

// 注入安全:恶意 RHS 只当字面量字符串比较,绝不执行 → 0 命中、applied=true、不抛
const inj = applyFilter(recs, '订单状态=已完成"); require("child_process").execSync("touch /tmp/pwned"); //');
eq('注入-当字面量(0命中)', inj.records.length, 0);
eq('注入-applied(已解析为字符串比较)', inj.applied, true);

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);
