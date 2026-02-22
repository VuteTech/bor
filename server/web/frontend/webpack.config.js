const webpackPath = require("path");
const InjectHtmlPlugin = require("html-webpack-plugin");
const CopyPlugin = require("copy-webpack-plugin");

const OUTPUT_FOLDER = webpackPath.resolve(__dirname, "..", "static");
const SOURCE_ENTRY = webpackPath.resolve(__dirname, "src", "main.tsx");
const HTML_SKELETON = webpackPath.resolve(__dirname, "src", "main.html");

const babelTransformRule = {
  test: /\.(tsx?|jsx?)$/,
  exclude: /node_modules/,
  use: {
    loader: "babel-loader",
    options: {
      presets: ["@babel/preset-typescript", ["@babel/preset-react", { runtime: "automatic" }]],
    },
  },
};

const cssInlineRule = {
  test: /\.css$/i,
  use: ["style-loader", "css-loader"],
};

const assetRule = {
  test: /\.(svg|png|jpg|jpeg|gif)$/,
  type: "asset/resource",
};

module.exports = {
  entry: SOURCE_ENTRY,
  output: {
    path: OUTPUT_FOLDER,
    filename: "bor.js",
    clean: false,
  },
  resolve: {
    extensions: [".tsx", ".ts", ".jsx", ".js"],
  },
  module: {
    rules: [babelTransformRule, cssInlineRule, assetRule],
  },
  plugins: [
    new InjectHtmlPlugin({
      template: HTML_SKELETON,
      filename: "index.html",
      favicon: webpackPath.resolve(__dirname, "src", "assets", "favicon.svg"),
    }),
    new CopyPlugin({
      patterns: [
        { from: webpackPath.resolve(__dirname, "src", "assets", "favicon.svg"), to: OUTPUT_FOLDER },
      ],
    }),
  ],
};
