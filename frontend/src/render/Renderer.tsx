// Renderer — 遍历 AppDefinition.ui.components 并分派到具体组件。
// 镜像 web/index.html 的 render(a)：stat / chart / text 三类，其余 (含 table) 走兜底卡片。
// table 组件的真实表格渲染留作后续扩展 (mock 也未实现)，此处给出占位说明。

import React from "react";
import type { AppDefinition, Component } from "../dsl";
import type { NormalizedRecord } from "../data";
import { Stat } from "./Stat";
import { Chart } from "./Chart";
import { Text } from "./Text";

export interface RendererProps {
  app: AppDefinition;
  records: NormalizedRecord[];
}

function renderComponent(
  c: Component,
  records: NormalizedRecord[],
  key: number,
): React.ReactNode {
  switch (c.type) {
    case "stat":
      return <Stat key={key} component={c} records={records} />;
    case "chart":
      return <Chart key={key} component={c} records={records} />;
    case "text":
      return <Text key={key} component={c} />;
    case "table":
      // 占位: 真实多列表格渲染尚未实现 (与 mock 一致)。
      return (
        <div key={key} className="card" style={{ gridColumn: "1 / -1" }}>
          <div className="t">{c.title ?? "表格"}</div>
          <div className="m">table 组件渲染待实现 (共 {records.length} 条记录)</div>
        </div>
      );
    default:
      // 防御: 未知组件类型，与 mock 的兜底分支一致。
      return (
        <div key={key} className="card">
          未知组件类型: {String((c as Component).type)}
        </div>
      );
  }
}

export const Renderer: React.FC<RendererProps> = ({ app, records }) => {
  const components = app.ui?.components ?? [];
  return (
    <div className="grid">
      {components.map((c, i) => renderComponent(c, records, i))}
    </div>
  );
};
