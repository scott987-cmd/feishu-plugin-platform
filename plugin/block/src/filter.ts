// 声明式筛选算子 —— 把 DSL component.filter(字符串)解析成谓词并对宿主记录求值。
// 安全红线:手写 tokenizer/parser,「绝不」用 eval/new Function;解析失败返回 null,
// 调用方据此「不过滤 + 告警」,而不是静默放行或执行任意代码。
//
// 支持的语法(v1):
//   条件     := LHS OP RHS
//   组合     := 条件 (AND 条件)*  (OR 条件组)*      // DNF:OR 连接的 AND 组
//   LHS      := 字段名 | month(字段) | year(字段) | day(字段)
//   OP       := = | != | > | >= | < | <= | contains | in
//   RHS      := 数字 | "字符串" | 裸词 | [a,b,c] | 宏(THIS_MONTH|THIS_YEAR|TODAY)
// 例:month(下单时间)=THIS_MONTH      订单状态=已完成      金额>=1000
//     订单状态 in [已完成,已付款]      year(下单时间)=THIS_YEAR AND 金额>500
import { toNumber, toLabel } from './cellValue';

export type Predicate = (rec: Record<string, unknown>) => boolean;

// 当前时间锚点(可注入以便单测);插件运行时用真实 new Date()。
let NOW: () => Date = () => new Date();
export function __setNowForTest(fn: () => Date): void { NOW = fn; }

function toDate(v: unknown): Date | null {
  if (v == null) return null;
  if (typeof v === 'number') { const d = new Date(v); return isNaN(d.getTime()) ? null : d; }
  if (typeof v === 'string') { const d = new Date(v); return isNaN(d.getTime()) ? null : d; }
  if (Array.isArray(v)) return v.length ? toDate((v[0] as any)?.text ?? (v[0] as any)?.timestamp ?? v[0]) : null;
  if (typeof v === 'object') { const o: any = v; return toDate(o.timestamp ?? o.text ?? o.value); }
  return null;
}

type Rhs =
  | { k: 'num'; v: number }
  | { k: 'str'; v: string }
  | { k: 'macro'; v: 'THIS_MONTH' | 'THIS_YEAR' | 'TODAY' }
  | { k: 'list'; v: Rhs[] };

function parseScalar(raw: string): Rhs {
  const s = raw.trim();
  if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
    return { k: 'str', v: s.slice(1, -1) };
  }
  const up = s.toUpperCase();
  if (up === 'THIS_MONTH' || up === 'THIS_YEAR' || up === 'TODAY') return { k: 'macro', v: up as any };
  if (s !== '' && !isNaN(Number(s))) return { k: 'num', v: Number(s) };
  return { k: 'str', v: s };
}

function parseRhs(raw: string): Rhs {
  const s = raw.trim();
  if (s.startsWith('[') && s.endsWith(']')) {
    const items = s.slice(1, -1).split(',').map((x) => x.trim()).filter(Boolean).map(parseScalar);
    return { k: 'list', v: items };
  }
  return parseScalar(s);
}

// 把 RHS 解析为可比较的数字(宏按需用当前时间)。
function rhsNum(r: Rhs): number | null {
  if (r.k === 'num') return r.v;
  if (r.k === 'macro') {
    const d = NOW();
    if (r.v === 'THIS_MONTH') return d.getMonth() + 1;
    if (r.v === 'THIS_YEAR') return d.getFullYear();
  }
  if (r.k === 'str') { const n = Number(r.v); return isNaN(n) ? null : n; }
  return null;
}
function rhsStr(r: Rhs): string {
  if (r.k === 'str') return r.v;
  if (r.k === 'num') return String(r.v);
  if (r.k === 'macro') return r.v;
  return '';
}
function rhsDate(r: Rhs): Date | null {
  if (r.k === 'macro' && r.v === 'TODAY') return NOW();
  if (r.k === 'str') return toDate(r.v);
  return null;
}

const LHS_FN = /^(month|year|day)\((.+)\)$/i;

function parseCond(raw: string): Predicate | null {
  const m = raw.match(/^(.+?)\s*(>=|<=|!=|=|>|<|\bcontains\b|\bin\b)\s*(.+)$/i);
  if (!m) return null;
  const lhsRaw = m[1].trim();
  const op = m[2].toLowerCase();
  const rhs = parseRhs(m[3]);
  const fn = lhsRaw.match(LHS_FN);

  // LHS = month/year/day(字段):取记录该日期字段的对应分量(数字),与 RHS 数字比较。
  if (fn) {
    const part = fn[1].toLowerCase();
    const field = fn[2].trim();
    return (rec) => {
      const d = toDate(rec[field]);
      if (!d) return false;
      const lv = part === 'month' ? d.getMonth() + 1 : part === 'year' ? d.getFullYear() : d.getDate();
      const rv = rhsNum(rhs);
      if (rv === null) return false;
      return cmpNum(lv, op, rv);
    };
  }

  // LHS = 普通字段名。
  const field = lhsRaw;
  if (op === 'contains') {
    const needle = rhsStr(rhs);
    return (rec) => toLabel(rec[field]).includes(needle);
  }
  if (op === 'in') {
    const items = rhs.k === 'list' ? rhs.v : [rhs];
    const set = new Set(items.map(rhsStr));
    return (rec) => set.has(toLabel(rec[field]));
  }
  if (op === '=' || op === '!=') {
    return (rec) => {
      const raw2 = rec[field];
      const rn = rhsNum(rhs);
      const ln = toNumber(raw2);
      const eq = rn !== null && ln !== null ? ln === rn : toLabel(raw2) === rhsStr(rhs);
      return op === '=' ? eq : !eq;
    };
  }
  // > >= < <= :优先数字,其次日期。
  return (rec) => {
    const raw2 = rec[field];
    const ln = toNumber(raw2);
    const rn = rhsNum(rhs);
    if (ln !== null && rn !== null) return cmpNum(ln, op, rn);
    const ld = toDate(raw2);
    const rd = rhsDate(rhs);
    if (ld && rd) return cmpNum(ld.getTime(), op, rd.getTime());
    return false;
  };
}

function cmpNum(a: number, op: string, b: number): boolean {
  switch (op) {
    case '>': return a > b;
    case '>=': return a >= b;
    case '<': return a < b;
    case '<=': return a <= b;
    case '=': return a === b;
    case '!=': return a !== b;
    default: return false;
  }
}

function splitTop(s: string, re: RegExp): string[] {
  return s.split(re).map((x) => x.trim()).filter(Boolean);
}

/** 解析筛选式 → 谓词;无法解析返回 null(调用方应不过滤并告警)。 */
export function parseFilter(expr: string | undefined): Predicate | null {
  if (!expr || !expr.trim()) return null;
  try {
    const orGroups = splitTop(expr, /\s+OR\s+/i);
    if (orGroups.length === 0) return null;
    const groups: Predicate[][] = [];
    for (const g of orGroups) {
      const conds = splitTop(g, /\s+AND\s+/i).map(parseCond);
      if (conds.some((c) => c === null)) return null;
      groups.push(conds as Predicate[]);
    }
    return (rec) => groups.some((grp) => grp.every((c) => c(rec)));
  } catch {
    return null;
  }
}

/** 对记录数组应用筛选式。expr 为空→原样返回;无法解析→返回 {records, error:true}(不过滤)。 */
export function applyFilter(
  records: Record<string, unknown>[],
  expr: string | undefined,
): { records: Record<string, unknown>[]; applied: boolean; error: boolean } {
  if (!expr || !expr.trim()) return { records, applied: false, error: false };
  const pred = parseFilter(expr);
  if (!pred) return { records, applied: false, error: true };
  return { records: records.filter(pred), applied: true, error: false };
}
