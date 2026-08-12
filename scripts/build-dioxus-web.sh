#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_DIR="$ROOT_DIR/app/dioxus-web"
OUTPUT_DIR="$ROOT_DIR/app/api/web/dist/dioxus"
BUILD_DIR="$PROJECT_DIR/target/dx/karmada-panel/release/web/public"

cd "$PROJECT_DIR"
dx build --release --web --debug-symbols false
gio trash "$OUTPUT_DIR" 2>/dev/null || true
mkdir -p "$OUTPUT_DIR"
cp -R "$BUILD_DIR"/. "$OUTPUT_DIR"/
# Dioxus CLI 不会把 assets/style.css 放入构建产物，而 index.html 引用了它
#（相对路径，加载 /dioxus/style.css）。手动复制到输出根目录。
cp "$PROJECT_DIR"/assets/style.css "$OUTPUT_DIR"/style.css
# Dioxus also injects the application title; preserve one canonical title.
sed -Ei 's|<title>[^<]*</title>|<title>Karmada Panel</title>|' "$OUTPUT_DIR"/index.html
# Dioxus 0.7.9 构建产物硬编码绝对路径 /./assets/，面板通过 /dioxus/ 路由访问时
# 这些路径指向 /assets/（404）而不是 /dioxus/assets/（200）。替换为相对路径。
sed -i 's|/\./assets/|./assets/|g' "$OUTPUT_DIR"/index.html "$OUTPUT_DIR"/assets/karmada-panel-*.js
