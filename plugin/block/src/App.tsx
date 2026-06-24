import React from 'react';
import { useAsync } from 'react-async-hook';
import { getApp, listApps, execute } from './api';
import { readHostData, currentTableId, aggregate, groupAggregate } from './bitableData';
import { resolveColumns, pivot, sortLimit } from './ops';
import { toLabel, toNumber } from './cellValue';
import { applyFilter } from './filter';
import type { AppDefinition, Component } from './dsl';

// 加载:从平台 API 取应用定义(?app=<id> 优先,否则取列表第一个),再用 SDK 读宿主真实数据。
async function load(): Promise<{ defs: AppDefinition[]; records: Record<string, unknown>[] }> {
  const id = new URLSearchParams(window.location.search).get('app');
  const apps = await listApps();
  let defs: AppDefinition[];
  if (id) {
    const d = await getApp(id);
    defs = d ? [d] : [];
  } else {
    // 多插件共存:渲染「绑定了当前数据表」的全部应用(按上架顺序堆叠);
    // 都不匹配时回退到第一个,保证空表场景仍有东西可看。
    const tid = await currentTableId().catch(() => undefined);
    const matched = tid ? apps.filter((a) => a.bind && a.bind.tableId === tid) : [];
    defs = matched.length ? matched : apps.slice(0, 1);
  }
  if (!defs.length) throw new Error('平台上没有可渲染的应用定义(先在平台生成一个)');
  // 同一宿主表:记录读一次,共享给该表上的所有插件。
  const { records } = await readHostData(defs[0]);
  return { defs, records };
}

const fmt = (n: number): string => Math.round(n).toLocaleString();

// 渲染器签名 —— 渲染器层注册表的统一类型(见下方 renderers)。
type Renderer = React.FC<{ c: Component; records: Record<string, unknown>[] }>;

// 高级版样式 —— 与平台前端同一套设计语言,作用域限定在 .fpp,避免污染飞书宿主。
const STYLE = `
.fpp{--brand:#0f6e56;--ink:#141a18;--muted:#6c7680;--faint:#9aa2ab;--line:rgba(18,26,24,.08);
  --warn-bg:#fef4e6;--warn-ink:#9a5b08;--warn-line:#f3d3a3;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB",system-ui,sans-serif;
  color:var(--ink);padding:16px;-webkit-font-smoothing:antialiased;}
.fpp .hd{display:flex;align-items:center;gap:9px;margin-bottom:16px}
.fpp .hd .mk{width:22px;height:22px;border-radius:7px;background:linear-gradient(145deg,#15916f,#0f6e56);flex:0 0 auto;box-shadow:0 3px 9px rgba(15,110,86,.3)}
.fpp .hd h1{font-size:15px;font-weight:650;letter-spacing:-.01em;margin:0}
.fpp .grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:14px}
.fpp .tile{background:#fff;border:1px solid var(--line);border-radius:16px;padding:18px;
  box-shadow:0 1px 2px rgba(18,26,24,.05),0 10px 26px rgba(18,26,24,.06);animation:rise .4s cubic-bezier(.2,.7,.3,1) both}
.fpp .tile.wide{grid-column:1/-1}
@keyframes rise{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:none}}
.fpp .lab{font-size:12px;color:var(--muted);font-weight:550}
.fpp .val{font-size:32px;font-weight:680;letter-spacing:-.02em;margin-top:8px;font-variant-numeric:tabular-nums}
.fpp .sub{display:inline-block;font-size:11px;color:var(--faint);font-family:ui-monospace,Menlo,monospace;margin-top:6px;background:#f3f5f7;border-radius:6px;padding:2px 7px}
.fpp .warn{display:inline-flex;align-items:center;gap:5px;font-size:11px;font-weight:550;color:var(--warn-ink);
  background:var(--warn-bg);border:1px solid var(--warn-line);border-radius:7px;padding:3px 8px;margin-top:10px;margin-left:6px}
.fpp .filt{display:inline-flex;align-items:center;gap:5px;font-size:11px;font-weight:550;color:#0a4a3a;
  background:#e9f6f0;border:1px solid #cdeadd;border-radius:7px;padding:3px 8px;margin-top:10px;margin-left:6px}
.fpp .ttl{display:flex;align-items:center;gap:8px;font-size:13px;font-weight:600}
.fpp .tag{font-size:10.5px;font-weight:600;color:#0a4a3a;background:#e9f6f0;border-radius:5px;padding:1px 7px;font-family:ui-monospace,Menlo,monospace}
.fpp .axis{font-size:11px;color:var(--faint);font-family:ui-monospace,Menlo,monospace;margin-left:auto}
.fpp .chart{position:relative;height:200px;margin-top:18px}
.fpp .cbody{position:absolute;left:0;right:0;top:0;bottom:22px}
.fpp .gl{position:absolute;left:0;right:0;border-top:1px dashed var(--line)}
.fpp .gl span{position:absolute;left:0;top:-8px;font-size:10px;color:var(--faint);font-variant-numeric:tabular-nums}
.fpp .bars{position:absolute;inset:0;display:flex;align-items:flex-end;gap:16px;padding-left:34px}
.fpp .bcol{flex:1;display:flex;flex-direction:column;align-items:center;justify-content:flex-end;height:100%;min-width:0}
.fpp .bv{font-size:11px;color:var(--muted);font-weight:550;margin-bottom:5px;font-variant-numeric:tabular-nums}
.fpp .bar{width:100%;max-width:54px;border-radius:7px 7px 3px 3px;background:linear-gradient(180deg,#1aa680,#0f6e56);
  box-shadow:0 4px 12px rgba(15,110,86,.22)}
.fpp .bx{font-size:11px;color:var(--muted);margin-top:8px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%}
.fpp .nodata{display:flex;align-items:center;justify-content:center;height:100%;color:var(--faint);font-size:13px}
.fpp .tblw{margin-top:14px;overflow-x:auto;border:1px solid var(--line);border-radius:12px}
.fpp .tbl{width:100%;border-collapse:collapse;font-size:12.5px}
.fpp .tbl th{text-align:left;font-weight:600;color:var(--muted);background:#f7f9fa;padding:9px 13px;border-bottom:1px solid var(--line);white-space:nowrap;position:sticky;top:0}
.fpp .tbl td{padding:8px 13px;border-bottom:1px solid var(--line);color:var(--ink);white-space:nowrap;max-width:280px;overflow:hidden;text-overflow:ellipsis;font-variant-numeric:tabular-nums}
.fpp .tbl tbody tr:last-child td{border-bottom:none}
.fpp .tbl tbody tr:hover td{background:#f7f9fa}
.fpp .tblmore{padding:9px 13px;font-size:11px;color:var(--faint);text-align:center;border-top:1px solid var(--line)}
.fpp .svgc{position:absolute;left:0;right:0;top:0;bottom:0;width:100%;height:100%}
.fpp .lgl{stroke:var(--line);stroke-width:1;stroke-dasharray:3 3}
.fpp .larea{fill:rgba(15,110,86,.08)}
.fpp .lline{fill:none;stroke:#0f6e56;stroke-width:2;stroke-linejoin:round}
.fpp .ldot{fill:#0f6e56}
.fpp .lcx{position:absolute;left:0;right:0;bottom:0;display:flex;justify-content:space-between;font-size:11px;color:var(--muted)}
.fpp .cbody.pie{display:flex;align-items:center;gap:26px;height:auto;bottom:auto}
.fpp .pielg{display:flex;flex-direction:column;gap:8px}
.fpp .pieli{display:flex;align-items:center;gap:8px;font-size:12px}
.fpp .pied{width:11px;height:11px;border-radius:3px;flex:0 0 auto}
.fpp .piek{color:var(--ink);min-width:88px}
.fpp .piev{color:var(--muted);font-variant-numeric:tabular-nums}
.fpp .gauge{display:flex;justify-content:center;margin-top:6px}
.fpp .gtrack{fill:none;stroke:#eef1f0;stroke-width:10}
.fpp .gfill{fill:none;stroke:#0f6e56;stroke-width:10;stroke-linecap:round;transition:stroke-dashoffset .5s ease}
.fpp .gpct{font-size:22px;font-weight:680;fill:var(--ink)}
.fpp .gsub{font-size:11px;fill:var(--muted)}
.fpp .tl{margin-top:14px;position:relative;padding-left:6px}
.fpp .tli{display:flex;gap:12px;padding:0 0 14px 14px;border-left:2px solid var(--line);position:relative}
.fpp .tli:last-child{border-left-color:transparent;padding-bottom:0}
.fpp .tldot{position:absolute;left:-7px;top:2px;width:11px;height:11px;border-radius:50%;background:linear-gradient(145deg,#15916f,#0f6e56);box-shadow:0 0 0 3px #fff}
.fpp .tlc{display:flex;flex-direction:column;gap:2px}
.fpp .tld{font-size:11px;color:var(--muted);font-variant-numeric:tabular-nums}
.fpp .tlt{font-size:13px;color:var(--ink);font-weight:550}
.fpp .kb{display:flex;gap:12px;margin-top:14px;overflow-x:auto;padding-bottom:4px}
.fpp .kbcol{flex:0 0 200px;background:#f7f9fa;border:1px solid var(--line);border-radius:12px;padding:10px}
.fpp .kbh{display:flex;align-items:center;justify-content:space-between;font-size:12.5px;font-weight:600;color:var(--ink);margin-bottom:8px}
.fpp .kbn{font-size:11px;color:var(--muted);background:#fff;border:1px solid var(--line);border-radius:10px;padding:0 7px;font-variant-numeric:tabular-nums}
.fpp .kbcards{display:flex;flex-direction:column;gap:8px}
.fpp .kbcard{background:#fff;border:1px solid var(--line);border-radius:9px;padding:9px 11px;box-shadow:0 1px 2px rgba(18,26,24,.04)}
.fpp .kbt{font-size:12.5px;font-weight:600;color:var(--ink)}
.fpp .kbf{font-size:11px;color:var(--muted);margin-top:3px}
.fpp .kbf span{color:var(--faint)}
.fpp .cd{display:flex;gap:10px;margin-top:10px}
.fpp .cdseg{display:flex;flex-direction:column;align-items:center;min-width:46px;background:#f3f5f7;border-radius:10px;padding:8px 4px}
.fpp .cdseg b{font-size:24px;font-weight:680;font-variant-numeric:tabular-nums;letter-spacing:-.02em}
.fpp .cdseg span{font-size:10px;color:var(--muted);margin-top:2px}
.fpp .md{font-size:13px;line-height:1.7;color:var(--ink)}
.fpp .md .mdh{font-weight:650;margin:10px 0 6px;letter-spacing:-.01em}
.fpp .md h1.mdh{font-size:18px}.fpp .md h2.mdh{font-size:15px}.fpp .md h3.mdh{font-size:13.5px}
.fpp .md .mdp{margin:6px 0}
.fpp .md .mdul{margin:6px 0;padding-left:20px}
.fpp .md .mdc{font-family:ui-monospace,Menlo,monospace;font-size:12px;background:#f3f5f7;border-radius:4px;padding:1px 5px}
.fpp .gal{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:12px;margin-top:14px}
.fpp .galc{background:#fff;border:1px solid var(--line);border-radius:11px;padding:12px 13px;box-shadow:0 1px 2px rgba(18,26,24,.04),0 8px 20px rgba(18,26,24,.05)}
.fpp .galt{font-size:13px;font-weight:600;color:var(--ink)}
.fpp .galf{font-size:11px;color:var(--muted);margin-top:4px}
.fpp .galf span{color:var(--faint)}
.fpp .calgrid{display:grid;grid-template-columns:repeat(7,1fr);gap:6px;margin-top:14px}
.fpp .calwk{text-align:center;font-size:11px;color:var(--muted);font-weight:600;padding:2px 0}
.fpp .calcell{position:relative;min-height:46px;border:1px solid var(--line);border-radius:8px;padding:5px 7px}
.fpp .calcell.empty{border-color:transparent;background:transparent}
.fpp .caldn{font-size:11px;color:var(--muted);font-variant-numeric:tabular-nums}
.fpp .calbadge{position:absolute;right:6px;bottom:6px;font-size:10px;font-weight:600;color:#fff;background:linear-gradient(145deg,#15916f,#0f6e56);border-radius:8px;padding:1px 7px}
.fpp .state{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;padding:56px 20px;text-align:center}
.fpp .state .sk{width:34px;height:34px;border:3px solid #e6e9ec;border-top-color:#0f6e56;border-radius:50%;animation:spin .8s linear infinite}
.fpp .state .big{font-size:14px;font-weight:600}
.fpp .state .sm{font-size:13px;color:var(--muted);max-width:300px}
.fpp .state.err .big{color:#b3261e}
@keyframes spin{to{transform:rotate(360deg)}}
`;

// 筛选徽标三态:无 filter → 不渲染;已应用 → 中性「已过滤」漏斗;解析失败 → 琥珀告警。
const FilterBadge: React.FC<{ c: Component; f: { applied: boolean; error: boolean } }> = ({ c, f }) => {
  if (!c.filter) return null;
  if (f.error) {
    return (
      <span className="warn" title={c.filter}>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round">
          <path d="M12 9v4M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
        </svg>
        过滤式无法解析
      </span>
    );
  }
  return (
    <span className="filt" title={c.filter}>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M22 3H2l8 9.46V19l4 2v-8.54L22 3z" />
      </svg>
      已过滤
    </span>
  );
};

const StatTile: React.FC<{ c: Component; records: Record<string, unknown>[] }> = ({ c, records }) => {
  const agg = c.agg ?? 'count';
  const f = applyFilter(records, c.filter);
  const val = aggregate(f.records, agg, c.field);
  return (
    <div className="tile">
      <div className="lab">{c.title ?? ''}</div>
      <div className="val">{fmt(val)}</div>
      <span className="sub">{`${agg}(${c.field ?? '—'})`}</span>
      <FilterBadge c={c} f={f} />
    </div>
  );
};

type CD = { label: string; value: number };

// chartType 子分派(chart 渲染器内的"小积木"):bar/line/pie 各一个 body。
const BarBody: React.FC<{ data: CD[]; max: number }> = ({ data, max }) => (
  <div className="cbody">
    {[1, 0.66, 0.33].map((ratio, i) => (
      <div className="gl" key={i} style={{ bottom: `${ratio * 100}%` }}><span>{fmt(max * ratio)}</span></div>
    ))}
    <div className="bars">
      {data.map((d, i) => (
        <div className="bcol" key={i}>
          <div className="bv">{fmt(d.value)}</div>
          <div className="bar" style={{ height: `${10 + (d.value / max) * 86}%` }} title={`${d.label}: ${fmt(d.value)}`} />
          <div className="bx">{d.label}</div>
        </div>
      ))}
    </div>
  </div>
);

const LineBody: React.FC<{ data: CD[]; max: number }> = ({ data, max }) => {
  const W = 600, H = 200, top = 16, bot = 26, plotH = H - top - bot;
  const n = data.length;
  const px = (i: number) => (n <= 1 ? W / 2 : (i * W) / (n - 1));
  const py = (v: number) => top + plotH * (1 - v / max);
  const line = data.map((d, i) => `${px(i)},${py(d.value)}`).join(' ');
  const area = `0,${top + plotH} ${line} ${W},${top + plotH}`;
  return (
    <div className="cbody">
      <svg className="svgc" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
        {[0, 0.33, 0.66, 1].map((rr, i) => {
          const y = top + plotH * (1 - rr);
          return <line key={i} x1={0} x2={W} y1={y} y2={y} className="lgl" vectorEffect="non-scaling-stroke" />;
        })}
        <polygon points={area} className="larea" />
        <polyline points={line} className="lline" vectorEffect="non-scaling-stroke" />
        {data.map((d, i) => (
          <circle key={i} cx={px(i)} cy={py(d.value)} r={3.5} className="ldot" vectorEffect="non-scaling-stroke">
            <title>{`${d.label}: ${fmt(d.value)}`}</title>
          </circle>
        ))}
      </svg>
      <div className="lcx">{data.map((d, i) => <span key={i}>{d.label}</span>)}</div>
    </div>
  );
};

const PIE_COLORS = ['#0f6e56', '#1aa680', '#5ac8a8', '#9bd9c4', '#2b8a72', '#c9ebe0'];
const PieBody: React.FC<{ data: CD[] }> = ({ data }) => {
  const total = data.reduce((a, d) => a + d.value, 0) || 1;
  const R = 70, r = 42, cx = 80, cy = 80;
  let acc = 0;
  return (
    <div className="cbody pie">
      <svg width={160} height={160} viewBox="0 0 160 160">
        {data.map((d, i) => {
          const a0 = (acc / total) * 2 * Math.PI; acc += d.value; const a1 = (acc / total) * 2 * Math.PI;
          const large = a1 - a0 > Math.PI ? 1 : 0;
          const x0 = cx + R * Math.sin(a0), y0 = cy - R * Math.cos(a0);
          const x1 = cx + R * Math.sin(a1), y1 = cy - R * Math.cos(a1);
          const xi1 = cx + r * Math.sin(a1), yi1 = cy - r * Math.cos(a1);
          const xi0 = cx + r * Math.sin(a0), yi0 = cy - r * Math.cos(a0);
          const path = `M${x0},${y0} A${R},${R} 0 ${large} 1 ${x1},${y1} L${xi1},${yi1} A${r},${r} 0 ${large} 0 ${xi0},${yi0} Z`;
          return (
            <path key={i} d={path} fill={PIE_COLORS[i % PIE_COLORS.length]}>
              <title>{`${d.label}: ${fmt(d.value)} (${Math.round((d.value / total) * 100)}%)`}</title>
            </path>
          );
        })}
      </svg>
      <div className="pielg">
        {data.map((d, i) => (
          <div className="pieli" key={i}>
            <span className="pied" style={{ background: PIE_COLORS[i % PIE_COLORS.length] }} />
            <span className="piek">{d.label}</span>
            <span className="piev">{fmt(d.value)} · {Math.round((d.value / total) * 100)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
};

const ChartTile: React.FC<{ c: Component; records: Record<string, unknown>[] }> = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const data = sortLimit(groupAggregate(f.records, c.x, c.y), c.sort, c.limit);
  const max = Math.max(1, ...data.map((d) => d.value));
  const yf = c.y ? `${c.y.agg}(${c.y.field ?? '—'})` : '';
  const kind = c.chartType ?? 'bar';
  return (
    <div className="tile wide">
      <div className="ttl">
        {c.title ?? ''}
        <span className="tag">{kind}</span>
        <span className="axis">{`x=${c.x ?? '—'} · y=${yf}`}</span>
      </div>
      <div className="chart">
        {data.length === 0 ? (
          <div className="nodata">无可聚合数据(检查字段名是否匹配宿主表)</div>
        ) : kind === 'pie' ? (
          <PieBody data={data} />
        ) : kind === 'line' ? (
          <LineBody data={data} max={max} />
        ) : (
          <BarBody data={data} max={max} />
        )}
      </div>
      {c.filter ? (
        <div style={{ marginTop: 14 }}>
          <FilterBadge c={c} f={f} />
        </div>
      ) : null}
    </div>
  );
};

const TABLE_CAP = 100; // 渲染行数上限(避免大表撑爆 DOM);超出时显式标注,不静默截断

const TableTile: Renderer = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const cols = resolveColumns(f.records, c.columns);
  const rows = f.records.slice(0, TABLE_CAP);
  return (
    <div className="tile wide">
      <div className="ttl">
        {c.title ?? ''}
        <span className="tag">table</span>
        <span className="axis">{`${f.records.length} 行 · ${cols.length} 列`}</span>
      </div>
      {f.records.length === 0 ? (
        <div className="nodata" style={{ height: 'auto', padding: '28px 0' }}>无数据</div>
      ) : (
        <div className="tblw">
          <table className="tbl">
            <thead>
              <tr>{cols.map((col, i) => <th key={i}>{col}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((r, ri) => (
                <tr key={ri}>{cols.map((col, ci) => <td key={ci} title={toLabel(r[col])}>{toLabel(r[col])}</td>)}</tr>
              ))}
            </tbody>
          </table>
          {f.records.length > TABLE_CAP ? (
            <div className="tblmore">显示前 {TABLE_CAP} 行,共 {f.records.length} 行</div>
          ) : null}
        </div>
      )}
      {c.filter ? <div style={{ marginTop: 12 }}><FilterBadge c={c} f={f} /></div> : null}
    </div>
  );
};

const TextTile: Renderer = ({ c }) => <div className="tile wide">{c.text ?? ''}</div>;

// gauge:进度/目标达成。value=对字段聚合(同 stat),target=目标常量;画进度环。
const GaugeTile: Renderer = ({ c, records }) => {
  const agg = c.agg ?? 'count';
  const f = applyFilter(records, c.filter);
  const val = aggregate(f.records, agg, c.field);
  const target = c.target && c.target > 0 ? c.target : 0;
  const pct = target > 0 ? Math.max(0, Math.min(1, val / target)) : 0;
  const R = 46, C = 2 * Math.PI * R, off = C * (1 - pct);
  return (
    <div className="tile">
      <div className="lab">{c.title ?? ''}</div>
      <div className="gauge">
        <svg width={128} height={128} viewBox="0 0 120 120">
          <circle cx={60} cy={60} r={R} className="gtrack" />
          <circle cx={60} cy={60} r={R} className="gfill" strokeDasharray={C} strokeDashoffset={off} transform="rotate(-90 60 60)" />
          <text x={60} y={57} textAnchor="middle" className="gpct">{target > 0 ? `${Math.round(pct * 100)}%` : '—'}</text>
          <text x={60} y={78} textAnchor="middle" className="gsub">{fmt(val)}{target > 0 ? ` / ${fmt(target)}` : ''}</text>
        </svg>
      </div>
      <span className="sub">{`${agg}(${c.field ?? '—'})${target > 0 ? ` 目标 ${fmt(target)}` : ''}`}</span>
      <FilterBadge c={c} f={f} />
    </div>
  );
};

// pivot:二维交叉聚合表(行=x、列=col、格=y 聚合)。
const PivotTile: Renderer = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const { rows, cols, grid } = pivot(f.records, c.x, c.col, c.y);
  const yf = c.y ? `${c.y.agg}(${c.y.field ?? '—'})` : '';
  return (
    <div className="tile wide">
      <div className="ttl">{c.title ?? ''}<span className="tag">pivot</span><span className="axis">{`行=${c.x ?? '—'} · 列=${c.col ?? '—'} · ${yf}`}</span></div>
      {rows.length === 0 || cols.length === 0 ? (
        <div className="nodata" style={{ height: 'auto', padding: '28px 0' }}>无可透视数据(需 x 行字段 + col 列字段 + y 度量)</div>
      ) : (
        <div className="tblw">
          <table className="tbl">
            <thead><tr><th>{c.x}</th>{cols.map((co, i) => <th key={i} style={{ textAlign: 'right' }}>{co}</th>)}</tr></thead>
            <tbody>
              {rows.map((rk, ri) => (
                <tr key={ri}><td>{rk}</td>{grid[ri].map((v, ci) => <td key={ci} style={{ textAlign: 'right' }}>{fmt(v)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {c.filter ? <div style={{ marginTop: 12 }}><FilterBadge c={c} f={f} /></div> : null}
    </div>
  );
};

// 把日期型 cell(ms 时间戳)格式化;否则纯文本。
const fmtDate = (v: unknown): string => {
  const n = toNumber(v);
  if (n && n > 1e11) { const d = new Date(n); if (!isNaN(d.getTime())) return d.toLocaleDateString('zh-CN'); }
  return toLabel(v);
};

// timeline:按日期字段排序的时间轴(只读)。field=日期列;columns[0]/x=条目标题列。
const TIMELINE_CAP = 60;
const TimelineTile: Renderer = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const dateField = (c.field ?? c.x) as string | undefined;
  const labelField = ((c.columns && c.columns[0]) || c.x) as string | undefined;
  const items = [...f.records]
    .sort((a, b) => (toNumber(b[dateField ?? '']) ?? 0) - (toNumber(a[dateField ?? '']) ?? 0))
    .slice(0, TIMELINE_CAP);
  return (
    <div className="tile wide">
      <div className="ttl">{c.title ?? ''}<span className="tag">timeline</span><span className="axis">{`按 ${dateField ?? '—'} 排序`}</span></div>
      {items.length === 0 ? (
        <div className="nodata" style={{ height: 'auto', padding: '24px 0' }}>无数据</div>
      ) : (
        <div className="tl">
          {items.map((r, i) => (
            <div className="tli" key={i}>
              <span className="tldot" />
              <div className="tlc">
                <div className="tld">{dateField ? fmtDate(r[dateField]) : ''}</div>
                <div className="tlt">{labelField ? toLabel(r[labelField]) : ''}</div>
              </div>
            </div>
          ))}
        </div>
      )}
      {c.filter ? <div style={{ marginTop: 12 }}><FilterBadge c={c} f={f} /></div> : null}
    </div>
  );
};

// kanban:只读看板。按 x 分列;每卡标题=columns[0]、明细=columns[1..3](省略则全字段)。
const KanbanTile: Renderer = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const groupField = c.x;
  const cols = resolveColumns(f.records, c.columns);
  const titleField = cols[0];
  const detailFields = cols.slice(1, 4);
  const groups = new Map<string, Record<string, unknown>[]>();
  if (groupField) for (const r of f.records) { const k = toLabel(r[groupField]); const a = groups.get(k) || []; a.push(r); groups.set(k, a); }
  const entries = Array.from(groups.entries());
  return (
    <div className="tile wide">
      <div className="ttl">{c.title ?? ''}<span className="tag">kanban</span><span className="axis">{`分列=${groupField ?? '—'}`}</span></div>
      {entries.length === 0 ? (
        <div className="nodata" style={{ height: 'auto', padding: '24px 0' }}>无数据(需 x 分列字段)</div>
      ) : (
        <div className="kb">
          {entries.map(([k, rows], ci) => (
            <div className="kbcol" key={ci}>
              <div className="kbh">{k}<span className="kbn">{rows.length}</span></div>
              <div className="kbcards">
                {rows.slice(0, 50).map((r, ri) => (
                  <div className="kbcard" key={ri}>
                    <div className="kbt">{titleField ? toLabel(r[titleField]) : '—'}</div>
                    {detailFields.map((fd, fi) => <div className="kbf" key={fi}><span>{fd}</span> {toLabel(r[fd])}</div>)}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {c.filter ? <div style={{ marginTop: 12 }}><FilterBadge c={c} f={f} /></div> : null}
    </div>
  );
};

// countdown:倒计时。text=目标时间字符串;解释器内置受控 tick(每秒),非用户 JS。
const CountdownTile: Renderer = ({ c }) => {
  const target = c.text ? new Date(c.text).getTime() : NaN;
  const [now, setNow] = React.useState<number>(() => Date.now());
  React.useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const valid = !Number.isNaN(target);
  const diff = valid ? Math.max(0, target - now) : 0;
  const dd = Math.floor(diff / 86400000), hh = Math.floor(diff / 3600000) % 24, mm = Math.floor(diff / 60000) % 60, ss = Math.floor(diff / 1000) % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    <div className="tile">
      <div className="lab">{c.title ?? ''}</div>
      {!valid ? <div className="val">—</div> : (
        <div className="cd">
          {[[dd, '天'], [pad(hh), '时'], [pad(mm), '分'], [pad(ss), '秒']].map(([v, u], i) => (
            <div className="cdseg" key={i}><b>{v}</b><span>{u}</span></div>
          ))}
        </div>
      )}
      <span className="sub">{valid ? `目标 ${new Date(target).toLocaleString('zh-CN')}` : '无效目标时间(填入 text,如 2026-12-31)'}</span>
    </div>
  );
};

// 安全 markdown:解析为 React 元素,绝不 innerHTML/dangerouslySetInnerHTML(数据通道 XSS 防线)。
const mdInline = (text: string): React.ReactNode[] => {
  const out: React.ReactNode[] = [];
  const re = /(\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`)/g;
  let last = 0, m: RegExpExecArray | null, key = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    if (m[2] != null) out.push(<strong key={key++}>{m[2]}</strong>);
    else if (m[3] != null) out.push(<em key={key++}>{m[3]}</em>);
    else if (m[4] != null) out.push(<code key={key++} className="mdc">{m[4]}</code>);
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
};
const Markdown: Renderer = ({ c }) => {
  const lines = (c.text ?? '').split('\n');
  const blocks: React.ReactNode[] = [];
  let list: string[] | null = null, key = 0;
  const flush = () => {
    if (list) { const items = list; blocks.push(<ul key={key++} className="mdul">{items.map((li, i) => <li key={i}>{mdInline(li)}</li>)}</ul>); list = null; }
  };
  for (const ln of lines) {
    if (/^\s*[-*]\s+/.test(ln)) { (list ||= []).push(ln.replace(/^\s*[-*]\s+/, '')); continue; }
    flush();
    const h = ln.match(/^(#{1,3})\s+(.*)$/);
    if (h) blocks.push(React.createElement(`h${h[1].length}`, { key: key++, className: 'mdh' }, mdInline(h[2])));
    else if (ln.trim() !== '') blocks.push(<p key={key++} className="mdp">{mdInline(ln)}</p>);
  }
  flush();
  return <div className="tile wide"><div className="md">{blocks}</div></div>;
};

// gallery:只读卡片墙。每记录一卡;标题=columns[0]、明细=columns[1..3]。
const GALLERY_CAP = 60;
const GalleryTile: Renderer = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const cols = resolveColumns(f.records, c.columns);
  const titleField = cols[0];
  const detailFields = cols.slice(1, 4);
  const items = f.records.slice(0, GALLERY_CAP);
  return (
    <div className="tile wide">
      <div className="ttl">{c.title ?? ''}<span className="tag">gallery</span><span className="axis">{`${f.records.length} 项`}</span></div>
      {items.length === 0 ? (
        <div className="nodata" style={{ height: 'auto', padding: '24px 0' }}>无数据</div>
      ) : (
        <div className="gal">
          {items.map((r, i) => (
            <div className="galc" key={i}>
              <div className="galt">{titleField ? toLabel(r[titleField]) : '—'}</div>
              {detailFields.map((fd, fi) => <div className="galf" key={fi}><span>{fd}</span> {toLabel(r[fd])}</div>)}
            </div>
          ))}
        </div>
      )}
      {c.filter ? <div style={{ marginTop: 12 }}><FilterBadge c={c} f={f} /></div> : null}
    </div>
  );
};

// calendar:月历格(只读)。按日期字段(field/x)把记录落到当月日格,显示当日条数。
const CalendarTile: Renderer = ({ c, records }) => {
  const f = applyFilter(records, c.filter);
  const dateField = (c.field ?? c.x) as string | undefined;
  const byDay = new Map<string, number>();
  let anchor: Date | null = null;
  if (dateField) {
    for (const r of f.records) {
      const n = toNumber(r[dateField]);
      if (n && n > 1e11) {
        const d = new Date(n);
        if (!Number.isNaN(d.getTime())) {
          const k = `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
          byDay.set(k, (byDay.get(k) || 0) + 1);
          if (!anchor) anchor = d;
        }
      }
    }
  }
  if (!anchor) anchor = new Date();
  const year = anchor.getFullYear(), month = anchor.getMonth();
  const startDow = new Date(year, month, 1).getDay();
  const days = new Date(year, month + 1, 0).getDate();
  const cells: (number | null)[] = [];
  for (let i = 0; i < startDow; i++) cells.push(null);
  for (let d = 1; d <= days; d++) cells.push(d);
  const wk = ['日', '一', '二', '三', '四', '五', '六'];
  return (
    <div className="tile wide">
      <div className="ttl">{c.title ?? ''}<span className="tag">calendar</span><span className="axis">{`${year}年${month + 1}月 · 按 ${dateField ?? '—'}`}</span></div>
      <div className="calgrid">
        {wk.map((w, i) => <div className="calwk" key={'w' + i}>{w}</div>)}
        {cells.map((d, i) => {
          const cnt = d ? byDay.get(`${year}-${month}-${d}`) : 0;
          return (
            <div className={`calcell${d ? '' : ' empty'}`} key={i}>
              {d ? <><span className="caldn">{d}</span>{cnt ? <span className="calbadge">{cnt}</span> : null}</> : null}
            </div>
          );
        })}
      </div>
      {c.filter ? <div style={{ marginTop: 12 }}><FilterBadge c={c} f={f} /></div> : null}
    </div>
  );
};

// 渲染器层(两层解释器之二)。加视图/图表 = 在此注册一个渲染器(并在 dsl 的 ComponentType 枚举登记),
// 容器解释器本体不变,故只审一次。键须与 dsl.VALID_COMPONENTS 对应。
// EnrichTile — 红区能力的演示渲染器:对每条可见记录,把 inputField 列的值作为
// formKey 入参调自托管 execute 运行时(POST /api/execute),把映射结果渲染成一行。
// 这是私有化下「字段捷径调外部 API」的真实闭环:数据来自我们 k8s 上的 execute-runner,
// 不经飞书 FaaS。每行一次调用,封顶 ENRICH_CAP 行避免打爆下游。
const ENRICH_CAP = 20;

const EnrichTile: Renderer = ({ c, records }) => {
  const inputField = c.inputField ?? '';
  const formKey = c.formKey ?? '';
  // toLabel turns empty cells into the literal "（空）" — treat that as empty so we
  // skip blank rows (otherwise we'd call execute with an empty value).
  const cityOf = (r: Record<string, unknown>): string => {
    const s = toLabel(r[inputField]);
    return s === '（空）' ? '' : s;
  };
  const rows = records.filter((r) => cityOf(r) !== '').slice(0, ENRICH_CAP);
  const res = useAsync(async () => {
    if (!c.executeDsl || !inputField || !formKey) {
      throw new Error('enrich 组件缺少 executeDsl / inputField / formKey');
    }
    return Promise.all(
      rows.map(async (r) => {
        const input = cityOf(r);
        if (!input) return { input: '', data: {} as Record<string, unknown> };
        try {
          const data = await execute(c.executeDsl, { [formKey]: input });
          return { input, data };
        } catch (e) {
          return { input, data: { __error: (e as Error).message } as Record<string, unknown> };
        }
      }),
    );
  }, []);

  // 输出列:优先 outputKeys;否则从首个成功结果推断(排除内部键 _id / __error)。
  const outCols: string[] = (() => {
    if (c.outputKeys && c.outputKeys.length) return c.outputKeys;
    const sample = (res.result ?? []).find((x) => x.data && !x.data.__error && Object.keys(x.data).length);
    return sample ? Object.keys(sample.data).filter((k) => k !== '_id' && k !== '__error') : [];
  })();

  return (
    <div className="tile wide">
      <div className="ttl">
        {c.title ?? ''}
        <span className="tag">enrich · execute-runtime</span>
        <span className="axis">{`${rows.length} 行`}</span>
      </div>
      {res.loading ? (
        <div className="nodata" style={{ height: 'auto', padding: '28px 0' }}>正在调用自托管 execute 运行时…</div>
      ) : res.error ? (
        <div className="warn" style={{ marginTop: 12 }}>{String((res.error as Error).message || res.error)}</div>
      ) : (
        <div className="tblw">
          <table className="tbl">
            <thead>
              <tr><th>{inputField}</th>{outCols.map((k, i) => <th key={i}>{k}</th>)}</tr>
            </thead>
            <tbody>
              {(res.result ?? []).map((row, ri) => (
                <tr key={ri}>
                  <td title={row.input}>{row.input}</td>
                  {row.data.__error ? (
                    <td colSpan={Math.max(outCols.length, 1)} className="lab" style={{ color: 'var(--warn-ink)' }}>
                      {toLabel(row.data.__error)}
                    </td>
                  ) : (
                    outCols.map((k, ci) => <td key={ci} title={toLabel(row.data[k])}>{toLabel(row.data[k])}</td>)
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
};

const renderers: Record<string, Renderer> = {
  stat: StatTile,
  chart: ChartTile,
  table: TableTile,
  gauge: GaugeTile,
  pivot: PivotTile,
  timeline: TimelineTile,
  kanban: KanbanTile,
  countdown: CountdownTile,
  markdown: Markdown,
  gallery: GalleryTile,
  calendar: CalendarTile,
  text: TextTile,
  enrich: EnrichTile,
};

const Comp: Renderer = ({ c, records }) => {
  const R = renderers[c.type];
  if (!R) return <div className="tile">未实现的组件类型:{c.type}</div>;
  return <R c={c} records={records} />;
};

// ErrorBoundary isolates a single plugin section: a malformed/legacy stored def
// or a throwing renderer degrades to a placeholder instead of blanking the whole
// panel (which renders every app bound to the table — one bad def must not take
// down the rest). Reset key lets it recover when the underlying def changes.
class ErrorBoundary extends React.Component<
  { resetKey?: unknown; children: React.ReactNode },
  { failed: boolean }
> {
  constructor(props: { resetKey?: unknown; children: React.ReactNode }) {
    super(props);
    this.state = { failed: false };
  }
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidUpdate(prev: { resetKey?: unknown }) {
    if (prev.resetKey !== this.props.resetKey && this.state.failed) {
      this.setState({ failed: false });
    }
  }
  render() {
    if (this.state.failed) {
      return <div className="tile warn">这个插件无法渲染(定义可能不兼容);其它插件不受影响。</div>;
    }
    return this.props.children;
  }
}

export const App = () => {
  const r = useAsync(load, []);
  return (
    <div className="fpp">
      <style>{STYLE}</style>
      {r.loading ? (
        <div className="state">
          <div className="sk" />
          <div className="sm">正在读取宿主表格数据…</div>
        </div>
      ) : null}
      {r.error ? (
        <div className="state err">
          <div className="big">无法渲染</div>
          <div className="sm">{String((r.error as Error).message || r.error)}</div>
        </div>
      ) : null}
      {r.result ? (
        <>
          {r.result.defs
            .filter((def) => def && def.ui && Array.isArray(def.ui.components))
            .map((def, di) => (
              <ErrorBoundary key={def.id ?? di} resetKey={def.id}>
                <section style={di ? { marginTop: 28 } : undefined}>
                  <div className="hd">
                    <div className="mk" aria-hidden="true" />
                    <h1>{def.name ?? '未命名插件'}</h1>
                  </div>
                  <div className="grid">
                    {def.ui.components.map((c, i) => (
                      <Comp key={i} c={c} records={r.result!.records} />
                    ))}
                  </div>
                </section>
              </ErrorBoundary>
            ))}
        </>
      ) : null}
    </div>
  );
};
