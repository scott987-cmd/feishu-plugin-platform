// 宿主数据层 —— 用官方 @lark-opdev/block-bitable-api 读当前多维表格的真实数据,
// 并按 DSL 的 agg/分组语义做聚合(与后端 generator 的语义一致)。
// SDK 边界处用宽松类型(any/unknown),逻辑才是重点;cell value 形态做防御式归一。

import { bitable, ViewType } from '@lark-opdev/block-bitable-api';
import type { AppDefinition } from './dsl';
// 兼容旧引用路径:归一函数移至 cellValue,聚合算子移至 ops(两层解释器的算子层),从本模块继续导出。
export { toNumber, toLabel } from './cellValue';
export { aggregate, groupAggregate } from './ops';

// records 以「字段名」为键(DSL 用字段名引用),值为该单元格原始 cellValue。
export interface HostData {
  records: Record<string, unknown>[];
}

// currentTableId 取插件被打开时所在的数据表 id(优先选区,其次活动表)。
// 用于「按当前表匹配应用定义」——同一容器渲染器支持多个插件:每个插件 bind 一张表,
// 打开哪张表就渲染绑定它的那个插件。解析失败返回 undefined(调用方回退 listApps()[0])。
export async function currentTableId(): Promise<string | undefined> {
  const base: any = bitable.base;
  try {
    const sel: any = await base.getSelection();
    if (sel && sel.tableId) return sel.tableId;
  } catch { /* ignore */ }
  try {
    if (typeof base.getActiveTable === 'function') {
      const t: any = await base.getActiveTable();
      if (t && t.id) return t.id;
    }
  } catch { /* ignore */ }
  return undefined;
}

// readHostData 解析宿主表(优先 bind.tableId,否则当前选区/活动表),读取可见记录。
export async function readHostData(def: AppDefinition): Promise<HostData> {
  const base: any = bitable.base;
  let tableId: string | undefined =
    def.bind && def.bind.tableId && def.bind.tableId !== '' && def.bind.tableId !== 'current'
      ? def.bind.tableId
      : undefined;
  if (!tableId) {
    const selection: any = await base.getSelection();
    tableId = selection && selection.tableId ? selection.tableId : undefined;
  }
  let table: any;
  if (tableId) {
    table = await base.getTableById(tableId);
  } else if (typeof base.getActiveTable === 'function') {
    table = await base.getActiveTable();
  }
  if (!table) {
    throw new Error('无法确定宿主数据表(请在多维表格中打开本插件)');
  }

  const views: any[] = await table.getViewMetaList();
  const gridMeta = views.find((v) => v.type === ViewType.Grid) || views[0];
  const view: any = await table.getViewById(gridMeta.id);
  const recordIds: (string | undefined)[] = await view.getVisibleRecordIdList();
  const fieldMetas: any[] = await view.getFieldMetaList();

  const records: Record<string, unknown>[] = [];
  for (const rid of recordIds) {
    if (!rid) continue;
    const row: Record<string, unknown> = {};
    for (const fm of fieldMetas) {
      row[fm.name] = await table.getCellValue(fm.id, rid);
    }
    records.push(row);
  }
  return { records };
}
