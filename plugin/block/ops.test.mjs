// ops.ts 单测:算子注册表(含新积木 median/distinct)+ groupAggregate。esbuild 打包后 node 跑。
import { build } from 'esbuild';

const out = await build({
  entryPoints: ['src/ops.ts'],
  bundle: true, format: 'esm', write: false, platform: 'node', logLevel: 'silent',
});
const mod = await import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));
const { aggregate, groupAggregate, aggregators, resolveColumns, pivot, sortLimit } = mod;

let pass = 0, fail = 0;
const eq = (n, g, w) => { const ok = g === w; console.log(`${ok ? '✓' : '✗'} ${n} => ${g}${ok ? '' : ` (want ${w})`}`); ok ? pass++ : fail++; };

const recs = [
  { 金额: 598, 数量: 2, 状态: '已完成' },
  { 金额: 1299, 数量: 1, 状态: '已付款' },
  { 金额: 599, 数量: 1, 状态: '已完成' },
];

// 原有 5 算子(行为须不变)
eq('sum', aggregate(recs, 'sum', '金额'), 2496);
eq('count', aggregate(recs, 'count'), 3);
eq('avg', aggregate(recs, 'avg', '金额'), 832);
eq('max', aggregate(recs, 'max', '金额'), 1299);
eq('min', aggregate(recs, 'min', '金额'), 598);
// 新积木
eq('median(奇数个)', aggregate(recs, 'median', '金额'), 599);
eq('median(偶数个)', aggregate([{ x: 10 }, { x: 20 }, { x: 30 }, { x: 40 }], 'median', 'x'), 25);
eq('distinct', aggregate(recs, 'distinct', '状态'), 2);
// 边界
eq('未知算子→0', aggregate(recs, 'bogus', '金额'), 0);
eq('空集 sum→0', aggregate([], 'sum', '金额'), 0);
// 分组聚合
const g = groupAggregate(recs, '状态', { agg: 'sum', field: '金额' });
eq('分组数', g.length, 2);
eq('已完成组 sum', (g.find((x) => x.label === '已完成') || {}).value, 1197);
// 注册表可扩展性(两层解释器地基)
eq('注册表含 median', typeof aggregators.median, 'function');
eq('注册表含 distinct', typeof aggregators.distinct, 'function');

// 新算子 range / stddev
eq('range', aggregate(recs, 'range', '金额'), 701); // 1299-598
eq('stddev(取整)', Math.round(aggregate(recs, 'stddev', '金额')), 330);

// pivot 二维聚合
const pr = [
  { 区: '华东', 品: 'A', 额: 10 }, { 区: '华东', 品: 'B', 额: 20 },
  { 区: '华北', 品: 'A', 额: 5 }, { 区: '华北', 品: 'B', 额: 7 },
];
const pv = pivot(pr, '区', '品', { agg: 'sum', field: '额' });
eq('pivot 行', pv.rows.join(','), '华东,华北');
eq('pivot 列', pv.cols.join(','), 'A,B');
eq('pivot 华东A', pv.grid[0][0], 10);
eq('pivot 华北B', pv.grid[1][1], 7);

// sortLimit(TopN)
const sl = sortLimit([{ label: 'a', value: 1 }, { label: 'b', value: 3 }, { label: 'c', value: 2 }], 'desc', 2);
eq('sortLimit TopN', sl.map((x) => x.label).join(','), 'b,c');
eq('sortLimit 长度', sl.length, 2);

// resolveColumns(table 渲染器用)
eq('列-显式', resolveColumns(recs, ['金额', '状态']).join(','), '金额,状态');
eq('列-省略→全字段', resolveColumns(recs).join(','), '金额,数量,状态');
eq('列-空记录→[]', resolveColumns([]).length, 0);
eq('列-空数组→全字段', resolveColumns(recs, []).join(','), '金额,数量,状态');

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);
