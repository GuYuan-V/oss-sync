服务器网页主题只改变登录、控制台和管理页面的视觉样式，不改变路由、权限、表单字段或数据。内置 `default` 主题只读；管理员可创建、上传、编辑、下载和删除自定义主题，普通用户只能在个人中心切换。

## 目录和文件

```text
data/console-themes/my-console/
├── theme.css
├── README.md
├── images/
│   └── background.webp
└── fonts/
    └── display.woff2
```

- `theme.css`：必需，页面在 `/ui/assets/console.css` 之后加载它。
- 图片：支持 PNG、JPEG、WebP 和 GIF。
- 字体：支持 WOFF、WOFF2、TTF 和 OTF。
- `README.md`：维护说明，不会加载到网页。

主题名必须是 1 至 64 个字符，以字母或数字开头，只能包含字母、数字、连字符和下划线。

## 修改颜色和背景

优先覆盖基础样式已经使用的变量：

```css
:root {
  --canvas: #f2efe8;
  --paper: #fffdf8;
  --ink: #172033;
  --muted: #647083;
  --line: #d8d1c5;
  --cobalt: #3159d9;
}

:root[data-theme="dark"] {
  --canvas: #10151f;
  --paper: #171e2a;
  --ink: #edf2fb;
  --muted: #9ba7ba;
  --line: #303b4e;
  --cobalt: #86a2ff;
}

body {
  background-color: var(--canvas);
  background-image: url("/ui/themes/my-console/images/background.webp");
}
```

控制台仍保留 `auto`、`light`、`dark` 三态切换。主题必须同时检查普通状态和 `:root[data-theme="dark"]`，不要只设计一种配色。

## 字体和资源

包内资源使用完整主题 URL：

```css
@font-face {
  font-family: "My Console Display";
  src: url("/ui/themes/my-console/fonts/display.woff2") format("woff2");
  font-display: swap;
}

.dashboard-heading h1 {
  font-family: "My Console Display", "Arial Narrow", sans-serif;
}
```

服务器只公开 CSS、常见图片和字体；HTML 与 JavaScript 不会作为主题资源执行。

## 保留的交互契约

自定义主题不得隐藏焦点轮廓、错误提示、表单标签或权限状态。需要检查 375px、768px 和 1280px 宽度，确保侧边栏、表格、模态框和代码编辑器没有页面级横向溢出。动画只使用 `transform`、`opacity` 或 `filter`，并尊重 `prefers-reduced-motion`。

数据卡片、设备表、近期活动和备份面板应继续使用基础主题提供的间距、边框和表格结构。主题可以改变颜色、字体和圆角，但不能把表格单元格改为 `display: flex`，否则列宽和分隔线会错位。

## Obsidian 插件样式边界

本指南只适用于服务器网页，不适用于 Obsidian 插件。插件运行在 Obsidian 宿主界面中，必须使用 Obsidian CSS 变量和控件样式；不要把服务器侧边栏、顶栏或全页背景复制到插件。

- 插件右侧栏是滚动容器，内容不能创建第二个页面级滚动条。
- 保留 Obsidian 的焦点、字体大小和高对比度行为。
- 插件新增状态、按钮和列表应优先复用宿主变量；自定义颜色只能补充状态，不能替代文字。

## 发布流程

1. 从 `default` 或现有主题创建副本，或者上传含 `theme.css` 的 ZIP。
2. 在线编辑 `theme.css`，也可下载 ZIP 后在本地维护资源。
3. 用普通用户在个人中心切换主题，检查亮色、暗色和窄屏。
4. 正在被用户选择的主题不能删除；先让这些用户切回其他主题。
5. 保存文件或重新上传后刷新网页即可生效，不需要重启服务。
