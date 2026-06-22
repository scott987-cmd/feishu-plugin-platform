// 平台 API 客户端 — 容器插件从平台读取它要渲染的 AppDefinition (DSL)。
// 对应后端 internal/api/server.go 的路由:
//   GET /api/apps        -> AppDefinition[]   (列表)
//   GET /api/apps/{id}   -> AppDefinition     (单个)
// 后端默认监听 :8080，CORS 由 withCORS 放开 (本地 ALLOWED_ORIGIN=*)。

import type { AppDefinition } from "./dsl";

// 最小 ambient 声明 —— 避免为读一个构建期注入的环境变量而引入 @types/node。
// webpack 用 DefinePlugin 注入时，process.env.PLATFORM_API_BASE 会被静态替换为字符串字面量。
declare const process: { env?: Record<string, string | undefined> } | undefined;

/**
 * 平台 API 基址。
 * 优先级: 构建期注入的 process.env.PLATFORM_API_BASE > 默认 http://localhost:8080。
 * 部署到线上时，应把它指向真实网关，并在后端设置具体的 ALLOWED_ORIGIN。
 *
 * 说明: webpack 默认不注入 process.env，这里做了存在性保护，未注入时回退默认值。
 */
export const API_BASE: string = ((): string => {
  try {
    const fromEnv =
      typeof process !== "undefined" && process && process.env
        ? process.env.PLATFORM_API_BASE
        : undefined;
    if (fromEnv) return String(fromEnv).replace(/\/+$/, "");
  } catch {
    // 忽略: 浏览器环境下 process 未定义。
  }
  return "http://localhost:8080";
})();

/**
 * 平台 API token —— 构建期注入 process.env.PLATFORM_API_TOKEN。后端设置了
 * PLATFORM_API_TOKEN 时,/api/* 要求 Authorization: Bearer。
 * 注意:前端内嵌的 token 对终端用户可见,仅适用于"企业内部自用、插件只发本企业"
 * 的场景;面向多用户/外部时应升级为用户级鉴权(见 PRODUCTION.md §7)。
 */
export const API_TOKEN: string = ((): string => {
  try {
    const t =
      typeof process !== "undefined" && process && process.env
        ? process.env.PLATFORM_API_TOKEN
        : undefined;
    if (t) return String(t);
  } catch {
    // 忽略: 浏览器环境下 process 未定义。
  }
  return "";
})();

/** 统一的 fetch + JSON 解析 + 错误规整。 */
async function getJSON<T>(path: string): Promise<T> {
  const url = `${API_BASE}${path}`;
  const headers: Record<string, string> = { Accept: "application/json" };
  if (API_TOKEN) headers.Authorization = `Bearer ${API_TOKEN}`;
  let resp: Response;
  try {
    resp = await fetch(url, {
      method: "GET",
      headers,
    });
  } catch (e) {
    throw new Error(`无法连接平台 API (${url}): ${(e as Error).message}`);
  }
  if (!resp.ok) {
    // 后端错误体形如 { "error": "..." } (writeErr)。
    let detail = `HTTP ${resp.status}`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body && body.error) detail = body.error;
    } catch {
      // 非 JSON 错误体，沿用状态码。
    }
    throw new Error(`平台 API 请求失败 (${url}): ${detail}`);
  }
  return (await resp.json()) as T;
}

/** 列出平台上已存储的全部应用定义。对应 GET /api/apps。 */
export function listApps(): Promise<AppDefinition[]> {
  return getJSON<AppDefinition[]>("/api/apps").then((apps) => apps ?? []);
}

/** 按 id 读取单个应用定义。对应 GET /api/apps/{id}。 */
export function getApp(id: string): Promise<AppDefinition> {
  if (!id) return Promise.reject(new Error("getApp: id 不能为空"));
  return getJSON<AppDefinition>(`/api/apps/${encodeURIComponent(id)}`);
}
