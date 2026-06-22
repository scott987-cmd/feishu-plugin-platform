// 插件入口 — 启动流程:
//   1. 取应用 id (URL ?app=xxx；否则取平台列表的第一个)
//   2. 从平台 API 拉该应用的 AppDefinition (DSL)
//   3. 用 @lark-base-open/js-sdk 读宿主表格真实记录
//   4. 用 Renderer 把 DSL 解释成 UI (真实数据)
// 全程含 loading / error 状态。
//
// 与 mock 渲染器 (web/index.html) 的本质区别：数据来自宿主多维表格，而非 mockVal()。

import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import type { AppDefinition } from "./dsl";
import { getApp, listApps } from "./api";
import { readRecords, type NormalizedRecord } from "./data";
import { Renderer } from "./render/Renderer";
import { STYLES } from "./styles";

type Phase =
  | { kind: "loading"; message: string }
  | { kind: "error"; message: string }
  | { kind: "ready"; app: AppDefinition; records: NormalizedRecord[] };

/** 从 URL query 读取 ?app=；无则返回 undefined。 */
function readAppIdFromUrl(): string | undefined {
  try {
    const sp = new URLSearchParams(window.location.search);
    const id = sp.get("app");
    return id ? id : undefined;
  } catch {
    return undefined;
  }
}

/** 解析要渲染的应用定义: 指定 id 优先，否则取列表第一个。 */
async function resolveAppDefinition(): Promise<AppDefinition> {
  const explicitId = readAppIdFromUrl();
  if (explicitId) return getApp(explicitId);

  const apps = await listApps();
  if (apps.length === 0) {
    throw new Error("平台上还没有任何应用定义 (GET /api/apps 为空)。请先在平台生成一个。");
  }
  return apps[0];
}

const App: React.FC = () => {
  const [phase, setPhase] = useState<Phase>({
    kind: "loading",
    message: "正在加载应用定义…",
  });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const app = await resolveAppDefinition();
        if (cancelled) return;
        setPhase({ kind: "loading", message: `正在读取宿主表格数据 (${app.name})…` });

        // automation 类型没有可渲染 UI，但仍展示其定义概要。
        const records = await readRecords(app.bind);
        if (cancelled) return;
        setPhase({ kind: "ready", app, records });
      } catch (e) {
        if (cancelled) return;
        setPhase({ kind: "error", message: (e as Error).message });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  if (phase.kind === "loading") {
    return (
      <div className="state">
        <div className="spinner" />
        <div className="state-msg">{phase.message}</div>
      </div>
    );
  }

  if (phase.kind === "error") {
    return (
      <div className="state state-error">
        <div className="state-title">加载失败</div>
        <div className="state-msg">{phase.message}</div>
        <div className="hint">
          请确认：① 平台 API 可达 (默认 http://localhost:8080)；
          ② 插件在飞书多维表格宿主中打开 (js-sdk 才能取到表格)；
          ③ 应用 bind 的 tableId 有效或为 current。
        </div>
      </div>
    );
  }

  const { app, records } = phase;
  return (
    <div className="app">
      <header className="hdr">
        <h1>{app.name}</h1>
        <p>
          <span className="pill">{app.type}</span>
          <span className="meta">
            {" "}
            v{app.version ?? 1} · {records.length} 条记录 · 布局 {app.ui?.layout ?? "—"}
          </span>
        </p>
      </header>
      <main className="body">
        <Renderer app={app} records={records} />
      </main>
    </div>
  );
};

// 注入样式 (移植自 mock 渲染器的配色与卡片/柱状图样式)。
const styleEl = document.createElement("style");
styleEl.textContent = STYLES;
document.head.appendChild(styleEl);

const container = document.getElementById("root");
if (!container) {
  throw new Error("缺少挂载根 #root");
}
createRoot(container).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
