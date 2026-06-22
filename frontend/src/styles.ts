// 插件样式 — 移植自 web/index.html 的配色与组件样式 (卡片 .card、柱状图 .bars/.bar)，
// 并补充 loading / error 状态样式。作为字符串注入 <style>，避免引入额外 CSS loader。

export const STYLES = `
:root {
  --bd: #e3e3e0;
  --mut: #6b6a66;
  --ac: #0F6E56;
}
* { box-sizing: border-box; }
body {
  font-family: -apple-system, "PingFang SC", system-ui, sans-serif;
  margin: 0;
  color: #23221f;
  background: #faf9f7;
}

.app { min-height: 100vh; }
.hdr {
  padding: 16px 24px;
  border-bottom: 1px solid var(--bd);
  background: #fff;
}
.hdr h1 { font-size: 18px; font-weight: 500; margin: 0; }
.hdr p { margin: 6px 0 0; color: var(--mut); font-size: 13px; }
.hdr .meta { color: var(--mut); }

.body { padding: 20px 24px; }

/* 组件网格 (与 mock .grid 一致) */
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 14px;
}

/* 卡片 (stat / text / 兜底) */
.card {
  border: 1px solid var(--bd);
  border-radius: 10px;
  padding: 16px;
  background: #fff;
}
.card .t { font-size: 13px; color: var(--mut); }
.card .v { font-size: 28px; font-weight: 500; margin-top: 6px; }
.card .m { font-size: 11px; color: #9b9a95; margin-top: 4px; }
.card .warn { font-size: 11px; color: #8a5a00; background: #fff4e0; border: 1px solid #f0d9a8; border-radius: 4px; padding: 2px 6px; margin-top: 8px; display: inline-block; }

/* 柱状图 (与 mock .bars/.bar 一致) */
.bars {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  height: 160px;
  margin-top: 10px;
}
.bar {
  flex: 1;
  background: var(--ac);
  border-radius: 4px 4px 0 0;
  min-height: 4px;
  position: relative;
}
.bar span {
  position: absolute;
  bottom: -18px;
  left: 0;
  right: 0;
  text-align: center;
  font-size: 11px;
  color: var(--mut);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pill {
  display: inline-block;
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 99px;
  background: #f0f8f5;
  color: var(--ac);
  border: 1px solid #cfe9df;
}

/* loading / error 状态 */
.state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  gap: 12px;
  padding: 24px;
  text-align: center;
}
.state-msg { color: var(--mut); font-size: 14px; max-width: 520px; }
.state-error .state-title,
.state-title { font-size: 16px; font-weight: 500; color: #c0392b; }
.state-error .state-msg { color: #c0392b; }
.hint {
  margin-top: 8px;
  font-size: 12px;
  color: #9b9a95;
  max-width: 520px;
  line-height: 1.6;
}
.spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--bd);
  border-top-color: var(--ac);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
`;
