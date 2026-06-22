import { testField, createFieldContext } from "@lark-opdev/block-basekit-server-api";
async function run() { const c = await createFieldContext(); const r = await testField({"text":"  hello world  "}, c as any); console.log("RESULT", JSON.stringify(r)); }
run();
