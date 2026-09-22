控制台主题只负责登录页、控制台和管理后台的视觉样式。功能、权限、表单和业务设置由插件提供。

  

## 1. 最小主题

  

创建目录：

  

```text

my-console-theme/

├── theme.css

└── README.md

```

  

`theme.css` 会在基础控制台 CSS 后加载。优先覆盖已有变量：

  

```css

:root {

  --canvas: #f2efe8;

  --paper: #fffdf8;

  --ink: #172033;

  --cobalt: #3159d9;

}

  

body { background: var(--canvas); color: var(--ink); }

```

  

可以携带图片和字体：

  

```text

my-console-theme/

├── theme.css

├── images/background.webp

└── fonts/display.woff2

```

  

主题无改变权限、路由或业务逻辑，也无功能设置。

  

## 2. 关联功能插件

  

如果主题需要设置项、后台功能、按钮或其他行为：

  

1. 先创建插件。

2. 在插件中声明设置和功能。

3. 将插件 ZIP 放到主题根目录，命名为 `plugin.zip`。

  

```text

my-console-theme.zip

├── theme.css

├── images/

├── fonts/

└── plugin.zip

```

  

上传主题时系统会自动安装、启用插件并记录关联。功能设置统一进入一级“插件设置”。

  

## 3. 上传

  

在主题目录内执行 `zip -r ../my-console-theme.zip .`，或在 PowerShell 中执行 `Compress-Archive * ../my-console-theme.zip`。然后在 **管理员设置 -> 服务器主题** 上传 ZIP，也可以从已有主题创建副本。检查亮色、暗色和窄屏布局。

  

主题负责外观；插件负责功能。