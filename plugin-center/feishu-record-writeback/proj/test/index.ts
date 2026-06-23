import { testAction, createActionContext } from "@lark-opdev/block-basekit-server-api";
// To run for real, fill in YOUR app credentials + a target Base/table (the plugin
// writes a real record). Inputs are entered by the user at config time in Feishu —
// they are never baked into the plugin.
async function run() {
  const c = await createActionContext();
  const r = await testAction({
    appId: "cli_xxxxxxxxxxxxxxxx",
    appSecret: "<your-app-secret>",
    appToken: "<target-base-app-token>",
    tableId: "<target-table-id>",
    title: "来自连接器的标题",
    content: "来自连接器的正文",
  }, c as any);
  console.log("RESULT", JSON.stringify(r)); // success → { code: 0, record_id: "rec..." }
}
run();
