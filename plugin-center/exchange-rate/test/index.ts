import { testField, createFieldContext } from "@lark-opdev/block-basekit-server-api";
async function run() { const c = await createFieldContext(); const r = await testField({"amount":"100"}, c as any); console.log("RESULT", JSON.stringify(r)); }
run();
