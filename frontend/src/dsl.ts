// TypeScript 镜像 — 与 Go 端 internal/dsl/dsl.go 的 AppDefinition 严格对应。
// JSON 字段名必须与 Go struct tag 完全一致 (id/name/type/bind/ui/...)，
// 因为本文件直接反序列化平台 API (/api/apps) 返回的 JSON。
//
// 这是平台的"硬通货"：同一份 AppDefinition 既驱动 Feishu 容器渲染器，也驱动
// opdev 导出编译。改动须与 dsl.go 同步。

/** 应用类型 — Go: ValidTypes */
export type AppType = "view_extension" | "automation";

/** 组件类型 — Go: ValidComponents */
export type ComponentType = "stat" | "chart" | "table" | "text";

/** 聚合方式 — Go: ValidAggs */
export type Agg = "sum" | "count" | "avg" | "max" | "min";

/** 图表类型 — Go: ValidCharts */
export type ChartType = "bar" | "line" | "pie";

/** 动作触发器 — Go: ValidTriggers */
export type Trigger = "button" | "onLoad";

/** 动作行为 — Go: ValidActions */
export type DoAction = "exportXlsx" | "notify";

/** UI 布局 — Go: UI.Layout (dashboard | list | form) */
export type Layout = "dashboard" | "list" | "form";

/**
 * Bind 把应用绑定到宿主多维表格 (Bitable)。
 * Go 注释: "current" 表示插件被打开时所在的 base，由 js-sdk 在客户端解析。
 */
export interface Bind {
  baseId: string;
  tableId: string;
}

/**
 * AggSpec — 对某字段的一次聚合，例如用作图表 Y 轴。
 * Go: AggSpec{ Agg, Field }
 */
export interface AggSpec {
  agg: Agg;
  field: string;
}

/**
 * Component — 一个可渲染单元。字段是各类型的超集；只有与 type 相关的字段会被使用。
 * Go: Component{ Type, Title?, Agg?, Field?, Filter?, ChartType?, X?, Y?, Text? }
 * 注意 omitempty 字段在 TS 侧设为可选 (?)。
 */
export interface Component {
  type: ComponentType;
  title?: string;
  agg?: Agg;
  field?: string;
  /**
   * 公式式过滤条件。Go 注释强调：filter 由后续的 Bitable 查询/导出引擎消费，
   * 必须在那一层被解析/白名单化，绝不可字符串插值。渲染器只透传展示。
   */
  filter?: string;
  chartType?: ChartType;
  x?: string;
  y?: AggSpec;
  text?: string;
}

/**
 * UI — 渲染器遍历的声明式组件树。
 * Go: UI{ Layout, Components }
 */
export interface UI {
  layout: Layout;
  components: Component[];
}

/**
 * Action — 声明式行为 (无任意代码)，渲染器把它接到某个触发器上。
 * Go: Action{ ID, Trigger, Label?, Do, Scope? }
 */
export interface Action {
  id: string;
  trigger: Trigger;
  label?: string;
  do: DoAction;
  scope?: string;
}

/**
 * AppDefinition — 单个生成的应用/插件，作为数据存储，运行时由容器插件渲染。
 * Go: AppDefinition{ ID, Name, Type, Bind, UI, Actions?, Version? }
 */
export interface AppDefinition {
  id: string;
  name: string;
  type: AppType;
  bind: Bind;
  ui: UI;
  actions?: Action[];
  version?: number;
}

// 与 Go 端 dsl.go 同步的枚举常量 (用于运行时校验/UI 提示)。
export const VALID_TYPES: AppType[] = ["view_extension", "automation"];
export const VALID_COMPONENTS: ComponentType[] = ["stat", "chart", "table", "text"];
export const VALID_AGGS: Agg[] = ["sum", "count", "avg", "max", "min"];
export const VALID_CHARTS: ChartType[] = ["bar", "line", "pie"];
export const VALID_ACTIONS: DoAction[] = ["exportXlsx", "notify"];
export const VALID_TRIGGERS: Trigger[] = ["button", "onLoad"];
