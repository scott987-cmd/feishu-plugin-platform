// Text — 对应 DSL component.type === "text"。纯文本说明卡片。
// 移植自 web/index.html 的 text 分支 (整行卡片)。

import React from "react";
import type { Component } from "../dsl";

export interface TextProps {
  component: Component;
}

export const Text: React.FC<TextProps> = ({ component }) => {
  // React 默认对 children 转义，等价于 mock 里的 escapeHtml。
  return (
    <div className="card" style={{ gridColumn: "1 / -1" }}>
      {component.text ?? ""}
    </div>
  );
};
