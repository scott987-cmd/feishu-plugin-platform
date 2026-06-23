import { testField, createFieldContext } from "@lark-opdev/block-basekit-server-api";
async function run() { const c = await createFieldContext(); const r = await testField({"idcard":"110101199003070011"}, c as any); console.log("RESULT", JSON.stringify(r)); }
run();
