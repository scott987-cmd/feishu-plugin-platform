import { testField, createFieldContext } from "@lark-opdev/block-basekit-server-api";
async function run() { const c = await createFieldContext(); const r = await testField({ text: "今天天气不错我们去公园玩吧" }, c as any); console.log("RESULT", JSON.stringify(r)); }
run();
