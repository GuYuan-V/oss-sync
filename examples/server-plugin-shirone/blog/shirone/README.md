# Shirone for OSS Sync

本模板作为 `shirone` 插件的 `blog` 资源安装，适配 OSS Sync `0.2.6` 当前模板接口；无需额外修改服务端。管理员在插件管理上传完整的 `shirone.zip` 并启用，然后在仓库设置选择 Shirone（`shirone--blog`）。名称、简介、头像、横幅和链接通过「插件设置 → Shirone 博客模板」保存。

公开首页包含当前仓库有效的单篇分享，每页展示 8 篇。文章页和文件夹分享使用服务端渲染正文；公开分享页从所属公开首页读取索引，提供搜索、分类、标签、日历及推荐。首页关闭时独立分享仅展示当前内容。模板不提供额外独立页面、音乐、评论或外部访问统计。

## 文章元数据

```yaml
---
title: 一篇笔记
description: 用于列表与搜索的摘要
published: 2026-10-02
category: 随笔
tags: [笔记, 生活]
image: ./附件/封面.webp
---
```

所有字段可省略，`summary`、`date`、`cover` 分别作为 `description`、`published`、`image` 的备用字段。有效 YAML frontmatter 由宿主隐藏；损坏或未闭合的块保留在正文。相对图片和封面使用服务端生成的授权 URL；用 `./` 明确表示文章相对路径。

复制按钮只在 `.AllowCopy` 为真时显示。显示设置按仓库存储在访客浏览器中。公开首页加载全部有效单篇分享后进行前端筛选、分页；首次处理成本随文章数量增长。

## 编辑与打包

源码位于项目 `examples/server-plugin-shirone/`，在项目根目录运行 `python3 examples/server-plugin-shirone/build.py` 生成完整插件包。上传包根必须有 `manifest.json` 和 `plugin.wasm`，本目录作为 `blog/shirone/` 放入包中，不单独压缩上传。

更新资源后须先提升插件 `manifest.json.version` 再构建上传，避免被宿主识别为重复包。使用 `.ThemeBaseURL` 引用资源；插件启用时生成的 `data/themes/shirone--blog/` 是派生副本。设置 schema 属于插件；模板 `theme.json` 只声明首页能力和允许公开的配置字段，不保存实际配置。

## 资源与许可

- Shirone 布局、令牌、纹理：`licenses/Shirone-MIT.txt`
- 完整 Maple Mono NF CN WOFF2 字体：`licenses/Maple-Mono-OFL.txt`，从参考项目完整 TTF 转换，未裁剪字形
- Material Symbols 图标：`licenses/Material-Symbols-Apache.txt`
- Material Color Utilities：`vendor/`、`material-color.source.js`
- Prism 按需代码高亮：`syntax-build.md`、`syntax.source.js`、`licenses/Prism-MIT.txt`
- 横幅：参考项目 `src/assets/images/banner/`，可在插件设置中替换

字体使用 `font-display: swap`，首次加载完成后呈现参考字体。全部页面资源随插件包提供。
