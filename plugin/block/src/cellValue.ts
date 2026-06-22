// 纯单元格值归一 —— 无飞书 SDK 依赖,可独立单测。被 bitableData(聚合)与 filter(筛选)共用。
// 飞书 cell value 形态多样(数字/字符串/数组 {text}/对象),这里尽力归一。

// toNumber 把多种 cell value 形态尽力归一为数字;不可解析返回 null。
export function toNumber(v: unknown): number | null {
  if (typeof v === 'number') return v;
  if (typeof v === 'string') {
    const n = Number(v);
    return Number.isNaN(n) ? null : n;
  }
  if (Array.isArray(v)) {
    const s = v.map((x: any) => (x && typeof x.text === 'string' ? x.text : '')).join('');
    const n = Number(s);
    return Number.isNaN(n) ? null : n;
  }
  if (v && typeof v === 'object') {
    const o: any = v;
    if (typeof o.text === 'string') {
      const n = Number(o.text);
      return Number.isNaN(n) ? null : n;
    }
  }
  return null;
}

// toLabel 把 cell value 归一为可读分组标签。
export function toLabel(v: unknown): string {
  if (v == null) return '（空）';
  if (typeof v === 'string' || typeof v === 'number') return String(v);
  if (Array.isArray(v)) {
    const s = v
      .map((x: any) => (x && x.text != null ? x.text : x && x.name != null ? x.name : ''))
      .join('');
    return s || '（空）';
  }
  if (typeof v === 'object') {
    const o: any = v;
    return String(o.text ?? o.name ?? '（空）');
  }
  return String(v);
}
