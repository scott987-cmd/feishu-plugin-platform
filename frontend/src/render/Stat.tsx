// Stat 卡片 — 对应 DSL component.type === "stat"。
// 移植自 web/index.html 的 .card 结构，但数值是 data.ts 用 js-sdk 读到的真实聚合值。

import React from "react";
import type { Component } from "../dsl";
import { aggregate, type NormalizedRecord } from "../data";

export interface StatProps {
  component: Component;
  records: NormalizedRecord[];
}

/** 大数字本地化格式 (与 mock 的 toLocaleString 一致)；非整数保留两位。 */
function formatValue(v: number): string {
  if (!Number.isFinite(v)) return "—";
  const rounded = Number.isInteger(v) ? v : Math.round(v * 100) / 100;
  return rounded.toLocaleString();
}

export const Stat: React.FC<StatProps> = ({ component, records }) => {
  const agg = component.agg ?? "count";
  const value = aggregate(records, agg, component.field);
  // 副标题: agg(field) · filter，与 mock 渲染保持一致 (filter 仅展示，不在前端执行)。
  const meta =
    `${agg}(${component.field ?? "?"})` +
    (component.filter ? ` · ${component.filter}` : "");

  return (
    <div className="card">
      <div className="t">{component.title ?? ""}</div>
      <div className="v">{formatValue(value)}</div>
      <div className="m">{meta}</div>
      {component.filter ? (
        // filter is NOT evaluated client-side yet — badge it so a filtered value
        // is not mistaken for a filtered result (e.g. a "本月" card showing all-time).
        <div className="warn" title={component.filter}>⚠ 过滤未执行(显示全量)</div>
      ) : null}
    </div>
  );
};
