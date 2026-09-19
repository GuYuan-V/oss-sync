A console theme owns visual styling for the login, dashboard, and admin pages. Features, permissions, forms, and business settings belong to a plugin.

## 1. Minimal theme

Create:

```text
my-console-theme/
├── theme.css
└── README.md
```

`theme.css` loads after the base console CSS. Override existing variables first:

```css
:root {
  --canvas: #f2efe8;
  --paper: #fffdf8;
  --ink: #172033;
  --cobalt: #3159d9;
}

body { background: var(--canvas); color: var(--ink); }
```

Images and fonts are allowed:

```text
my-console-theme/
├── theme.css
├── images/background.webp
└── fonts/display.woff2
```

Themes must not change permissions, routes, or business behavior.

## 2. Link a feature plugin

For settings, admin functions, buttons, or behavior:

1. Create the plugin first.
2. Declare its settings and features.
3. Put the plugin ZIP at the theme root as `plugin.zip`.

```text
my-console-theme.zip
├── theme.css
├── images/
├── fonts/
└── plugin.zip
```

Uploading the theme installs, enables, and associates the plugin automatically. Functional settings appear in the top-level Plugin settings menu.

## 3. Upload

From inside the theme directory, run `zip -r ../my-console-theme.zip .`, or use `Compress-Archive * ../my-console-theme.zip` in PowerShell. Upload the ZIP from **Admin settings -> Console themes**, or create a copy from an existing theme. Check light, dark, and narrow layouts.

Themes provide appearance. Plugins provide functionality.
