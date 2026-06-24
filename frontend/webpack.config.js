// Webpack 配置 — Feishu 数据表视图插件官方默认栈 (React + TS + Webpack)。
// 入口 src/index.tsx，用 ts-loader 编译，html-webpack-plugin 注入挂载页。
const path = require("path");
const webpack = require("webpack");
const HtmlWebpackPlugin = require("html-webpack-plugin");

module.exports = (_env, argv) => {
  const isProd = argv && argv.mode === "production";
  return {
    entry: "./src/index.tsx",
    output: {
      path: path.resolve(__dirname, "dist"),
      filename: "[name].[contenthash:8].js",
      clean: true,
      // 插件被宿主页面以 iframe 形式嵌入，资源用相对路径以适配 opdev 上传后的部署根。
      publicPath: "./",
    },
    resolve: {
      extensions: [".tsx", ".ts", ".jsx", ".js"],
    },
    module: {
      rules: [
        {
          test: /\.tsx?$/,
          use: "ts-loader",
          exclude: /node_modules/,
        },
      ],
    },
    plugins: [
      new HtmlWebpackPlugin({
        template: "./src/index.html",
      }),
      // Statically inject the platform API base + bearer token at build time so
      // src/api.ts can read them (the Feishu webview has no process.env at runtime).
      // Usage: PLATFORM_API_BASE=https://... PLATFORM_API_TOKEN=... npm run build
      new webpack.DefinePlugin({
        "process.env.PLATFORM_API_BASE": JSON.stringify(process.env.PLATFORM_API_BASE || ""),
        // ONLY the read-only token is injected; the admin PLATFORM_API_TOKEN is
        // deliberately never defined here so it cannot be baked into a browser bundle.
        "process.env.PLATFORM_READ_TOKEN": JSON.stringify(process.env.PLATFORM_READ_TOKEN || ""),
      }),
    ],
    devtool: isProd ? false : "source-map",
    devServer: {
      // opdev 本地预览默认期望插件跑在 dev server 上；端口可按官方工具要求调整。
      static: path.resolve(__dirname, "dist"),
      port: 3000,
      hot: true,
      // 允许 Feishu 客户端 webview 跨域加载本地预览资源。
      headers: { "Access-Control-Allow-Origin": "*" },
      historyApiFallback: true,
    },
  };
};
