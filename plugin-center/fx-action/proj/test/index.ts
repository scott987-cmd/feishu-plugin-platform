import { testAction, createActionContext } from "@lark-opdev/block-basekit-server-api";
async function run() { const c = await createActionContext(); const r = await testAction({"amount":"100"}, c as any); console.log("RESULT", JSON.stringify(r)); }
run();
