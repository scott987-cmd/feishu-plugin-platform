// 宿主数据层 — 用 @lark-base-open/js-sdk 读取 Feishu 多维表格 (Bitable) 的真实数据，
// 并提供与 DSL 语义一致的聚合工具 (sum/count/avg/max/min) 及图表分组聚合。
//
// 这是 mock 渲染器 (web/index.html) 与真实插件的关键差异：mock 用 mockVal() 造数，
// 这里换成宿主表格真实记录。
//
// SDK 参考文档: https://lark-base-team.github.io/js-sdk-docs/
// 下方使用的都是文档常见 API：
//   bitable.base.getActiveTable() / bitable.base.getTableById(tableId)
//   table.getFieldMetaList()      -> 字段元信息 (id/name/type)
//   table.getRecords({ pageSize, pageToken }) -> 分页记录，返回 { records, pageToken, hasMore }
//   record.fields[fieldId]        -> 单元格原始值 (各字段类型为不同联合类型)
// ⚠️ 不同 SDK 版本的分页/单元格返回结构可能略有差异，集成时请对照上面文档核验
//    (尤其 getRecords 的入参/返回字段名，以及富文本/数字字段的取值形状)。

import { bitable } from "@lark-base-open/js-sdk";
import type { Agg, AggSpec, Bind } from "./dsl";

/**
 * 归一化后的记录 —— 把 SDK 异构的单元格值压平成 { 字段名 -> 标量 }。
 * 这样聚合/分组逻辑无需关心字段类型联合，且按"字段名"查找 (DSL 里写的是字段名)。
 */
export interface NormalizedRecord {
  recordId: string;
  /** key 为字段名 (field name)，value 为尽力归一化的标量。 */
  cells: Record<string, ScalarCell>;
}

export type ScalarCell = number | string | boolean | null;

/** 字段名 -> 字段 id 的映射 (SDK record.fields 以 fieldId 为 key)。 */
type FieldNameToId = Map<string, string>;

/**
 * 本插件实际用到的表格表面 —— 只声明我们调用的方法，与 SDK 完整 ITable 解耦。
 * 这样即便不同 SDK 版本的 ITable 类型有出入，本层代码仍类型自洽；
 * 真实形状以官方文档为准: https://lark-base-team.github.io/js-sdk-docs/
 */
interface TableSurface {
  getFieldMetaList(): Promise<Array<{ id: string; name: string }>>;
  getRecords(params: { pageSize: number; pageToken?: string }): Promise<{
    records?: Array<{ recordId: string; fields: Record<string, unknown> }>;
    pageToken?: string;
    hasMore?: boolean;
  }>;
}

/** 解析活动表格 (或按 bind.tableId 指定的表)。 */
export async function resolveTable(bind?: Bind): Promise<TableSurface> {
  // bind.tableId 为空 / "current" 时，用插件被打开时所在的活动表。
  const tableId = bind?.tableId?.trim();
  // 经一次 unknown 收窄到 TableSurface：SDK 返回的是完整 ITable，这里只取我们用到的子集。
  if (tableId && tableId !== "current") {
    // getTableById 在表不存在时会抛错，交由上层 catch 展示。
    return (await bitable.base.getTableById(tableId)) as unknown as TableSurface;
  }
  return (await bitable.base.getActiveTable()) as unknown as TableSurface;
}

/** 读取字段元信息，构建 字段名 -> 字段id 映射。 */
async function buildFieldIndex(table: TableSurface): Promise<FieldNameToId> {
  const metas = await table.getFieldMetaList();
  const idx: FieldNameToId = new Map();
  for (const m of metas) {
    // m: { id, name, type, ... }；以 name 为 key，后出现者覆盖 (与同名字段冲突时取最后一个)。
    if (m && typeof m.name === "string") idx.set(m.name, m.id);
  }
  return idx;
}

/**
 * 把 SDK 的原始单元格值尽力归一化为标量。
 * 覆盖常见形状：number / string / boolean / null；
 *  - 文本/富文本: [{ type, text }] 或 { text } -> 拼接文本
 *  - 数字: 直接为 number
 *  - 单选: { id, text } -> text；多选: [{ text }, ...] -> 逗号拼接
 *  - 人员/链接等复杂类型: 退化为其 text / name，否则 String() 兜底
 * 取不到有意义标量时返回 null。
 */
function normalizeCellValue(raw: unknown): ScalarCell {
  if (raw == null) return null;
  if (typeof raw === "number" || typeof raw === "boolean") return raw;
  if (typeof raw === "string") return raw;

  // 数组: 富文本段 / 多选 / 人员列表等。
  if (Array.isArray(raw)) {
    const parts = raw
      .map((seg) => {
        if (seg == null) return "";
        if (typeof seg === "string" || typeof seg === "number") return String(seg);
        const o = seg as Record<string, unknown>;
        const t = o.text ?? o.name ?? o.enName ?? o.id;
        return t == null ? "" : String(t);
      })
      .filter((s) => s !== "");
    return parts.length ? parts.join(", ") : null;
  }

  // 对象: 单选 / 人员 / 数字带格式 等。
  if (typeof raw === "object") {
    const o = raw as Record<string, unknown>;
    if (typeof o.text === "string") return o.text;
    if (typeof o.name === "string") return o.name as string;
    if (typeof o.value === "number" || typeof o.value === "string") {
      return o.value as ScalarCell;
    }
    return null;
  }
  return null;
}

/**
 * 分页读取宿主表格全部记录，归一化为 NormalizedRecord[]。
 * @param maxRecords 上限保护，默认 5000，避免超大表把内存/请求打满。
 *                   SDK 单页 pageSize 上限通常为 5000 (以文档为准)，这里按页累计直到达到 maxRecords。
 */
export async function readRecords(
  bind?: Bind,
  maxRecords = 5000,
): Promise<NormalizedRecord[]> {
  const table = await resolveTable(bind);
  const fieldIndex = await buildFieldIndex(table);

  // 字段id -> 字段名 反查 (record.fields 以 fieldId 为 key)。
  const idToName = new Map<string, string>();
  for (const [name, id] of fieldIndex) idToName.set(id, name);

  const out: NormalizedRecord[] = [];
  let pageToken: string | undefined = undefined;
  const pageSize = 200; // 保守的单页大小，分页累加。

  // 防御: 限制最大翻页轮数，避免极端情况下死循环。
  for (let guard = 0; guard < 1000; guard++) {
    // getRecords 返回 { records, pageToken, hasMore } (字段名以 SDK 文档为准)。
    const page = await table.getRecords({ pageSize, pageToken });

    const records = page?.records ?? [];
    for (const r of records) {
      const cells: Record<string, ScalarCell> = {};
      const fields = r.fields ?? {};
      for (const fieldId of Object.keys(fields)) {
        const name = idToName.get(fieldId);
        if (!name) continue; // 未在元信息中出现的字段忽略。
        cells[name] = normalizeCellValue(fields[fieldId]);
      }
      out.push({ recordId: r.recordId, cells });
      if (out.length >= maxRecords) return out;
    }

    if (!page?.hasMore || !page.pageToken) break;
    pageToken = page.pageToken;
  }
  return out;
}

/** 把一个标量单元格尽力转成数字 (用于 sum/avg/max/min)；无法转换返回 null。 */
function toNumber(v: ScalarCell): number | null {
  if (v == null || typeof v === "boolean") return null;
  if (typeof v === "number") return Number.isFinite(v) ? v : null;
  const n = Number(String(v).replace(/,/g, "").trim());
  return Number.isFinite(n) ? n : null;
}

/**
 * 标量聚合 —— 对应 DSL 的 stat 组件 (agg + field)。
 * 语义与 Go DSL 一致:
 *   - count: 记录条数 (field 为空也成立；与 mock 不同，这里是真实条数)
 *   - sum/avg/max/min: 仅统计 field 可转为数字的记录，缺失/非数字跳过 (防御式 -> 0)
 * 空数据或全部缺失时返回 0。
 */
export function aggregate(
  records: NormalizedRecord[],
  agg: Agg,
  field?: string,
): number {
  if (agg === "count") {
    // 有 field 时，按"该字段非空"计数；无 field 时计全部记录数。
    if (!field) return records.length;
    return records.reduce(
      (acc, r) => acc + (r.cells[field] != null && r.cells[field] !== "" ? 1 : 0),
      0,
    );
  }

  if (!field) return 0; // sum/avg/max/min 必须有 field。

  const nums: number[] = [];
  for (const r of records) {
    const n = toNumber(r.cells[field]);
    if (n != null) nums.push(n);
  }
  if (nums.length === 0) return 0;

  switch (agg) {
    case "sum":
      return nums.reduce((a, b) => a + b, 0);
    case "avg":
      return nums.reduce((a, b) => a + b, 0) / nums.length;
    case "max":
      return Math.max(...nums);
    case "min":
      return Math.min(...nums);
    default:
      // 穷尽枚举后的兜底 (类型上不可达)。
      return 0;
  }
}

/** 图表分组聚合的一项: 一个 x 分类 + 其聚合后的 y 值。 */
export interface GroupedPoint {
  label: string;
  value: number;
}

/**
 * 分组聚合 —— 对应 DSL 的 chart 组件 (x + y{agg,field})。
 * 按 xField 的值分组，每组对 y 做聚合。等价于 SQL 的 GROUP BY x 后聚合 y。
 * 缺失 x 的记录归入 "(空)" 分组；y 字段缺失/非数字按聚合语义跳过。
 * 结果按 value 降序排列 (常见看板习惯)，便于 bar 渲染。
 */
export function groupAggregate(
  records: NormalizedRecord[],
  xField: string | undefined,
  y: AggSpec | undefined,
): GroupedPoint[] {
  if (!xField || !y) return [];

  // 先把记录按 x 分桶。
  const buckets = new Map<string, NormalizedRecord[]>();
  for (const r of records) {
    const xv = r.cells[xField];
    const label = xv == null || xv === "" ? "(空)" : String(xv);
    const arr = buckets.get(label);
    if (arr) arr.push(r);
    else buckets.set(label, [r]);
  }

  const points: GroupedPoint[] = [];
  for (const [label, group] of buckets) {
    points.push({ label, value: aggregate(group, y.agg, y.field) });
  }
  points.sort((a, b) => b.value - a.value);
  return points;
}
