A blog template owns page structure and visual styling only. Features, data, dynamic behavior, and functional settings belong to a linked server plugin.

## 1. Minimal template

Create this directory:

```text
my-template/
├── template.html
├── style.css
├── theme.js
└── theme.json
```

Use this minimal `template.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="{{.ThemeBaseURL}}/style.css">
</head>
<body>
  <main>{{.ContentHTML}}</main>
  <script src="{{.ThemeBaseURL}}/theme.js" defer></script>
</body>
</html>
```

Put colors, spacing, and layout in `style.css`. Put page interactions in `theme.js`. Templates can read `.Title`, `.ContentHTML`, `.ThemeBaseURL`, `.HomePosts`, `.BlogName`, and `.ThemeConfigJS`. `.ThemeConfigJS` comes from the linked plugin, not from the template.

`theme.json` is optional and only declares capabilities:

```json
{"supports_public_blog": true}
```

Do not create `settings.json`. Templates do not own functional settings.

## 2. Link a feature plugin

For forms, analytics, comments, external APIs, or any other behavior:

1. Create a server plugin first.
2. Declare its settings, hooks, routes, or admin pages.
3. Build the plugin ZIP.
4. Put that ZIP in the template root as `plugin.zip`.

Final package:

```text
my-template.zip
├── template.html
├── style.css
├── theme.js
├── theme.json
└── plugin.zip
```

Uploading the template installs, enables, and associates the plugin automatically. Plugin settings appear in the top-level Plugin settings menu; the template only displays the resulting values.

## 3. Upload

From inside the template directory, run `zip -r ../my-template.zip .`, or use `Compress-Archive * ../my-template.zip` in PowerShell. Upload the ZIP from **Admin settings -> Templates**, or create a copy from an existing template. Names may contain letters, digits, hyphens, and underscores.

Templates provide presentation. Plugins provide functionality.
