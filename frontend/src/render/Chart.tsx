// Chart — 对应 DSL component.type === "chart"。
// 移植自 web/index.html 的柱状图 (.bars/.bar，纯 div 高度)，但分类与高度来自真实分组聚合。
// chartType line/pie 当前以 bar 形态降级渲染 (与 mock 一致：mock 也只画 bar)，
// 标题里仍标注原始 chartType，便于后续扩展真实 line/pie 实现。

import React from "react";
import type { Component } from "../dsl";
import { groupAggregate, type NormalizedRecord } from "../data";

export interface ChartProps {
  component: Component;
  records: NormalizedRecord[];
}

const MAX_BARS = 12; // 防御: 分类过多时只画前若干个 (已按 value 降序)。
const BAR_AREA_PX = 160; // 与 mock 的 .bars 高度一致。

export const Chart: React.FC<ChartProps> = ({ component, records }) => {
  const points = groupAggregate(records, component.x, component.y).slice(0, MAX_BARS);
  const max = points.reduce((m, p) => (p.value > m ? p.value : m), 0);

  const yLabel = component.y ? `${component.y.agg}(${component.y.field ?? "?"})` : "";
  const chartType = component.chartType ?? "bar";
  const header =
    `${component.title ?? ""} · ${chartType} · x=${component.x ?? "?"} y=${yLabel}`;

  return (
    <div className="card" style={{ gridColumn: "1 / -1" }}>
      <div className="t">{header}</div>
      {points.length === 0 ? (
        <div className="m" style={{ marginTop: 10 }}>
          无数据 / 缺少 x 或 y 配置
        </div>
      ) : (
        <div className="bars">
          {points.map((p) => {
            // 高度按 value 占最大值的比例映射到柱区 (最小 4px，保证可见)。
            const h = max > 0 ? Math.max(4, Math.round((p.value / max) * BAR_AREA_PX)) : 4;
            return (
              <div
                key={p.label}
                className="bar"
                style={{ height: `${h}px` }}
                title={`${p.label}: ${p.value.toLocaleString()}`}
              >
                <span>{p.label}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
