> This guide is AI-optimized (AI-Friendly / LLM-Readable): the field contract, render rules, and failure fallback are given as explicit lists and tables for humans and AI to copy directly. Read the hard rules in section 0 before building a template.

A blog template owns page structure and visual styling only. Features, data, dynamic behavior, and functional settings belong to a linked server plugin.

## 0. Hard rules (read before building)

- Custom templates use Go `html/template` and render with `missingkey=error`.
- Referencing any field or sub-field the server does not provide (a `.Foo` or `.ArticlePost.Bar` that is not on the struct) makes the whole page fail to render.
- On failure the server does not surface an error to visitors; it **silently falls back to the built-in `default` theme** — the page looks like the built-in default, not your template. This is commonly misread as "the template didn't apply" or "it looks the same as other themes".
- Therefore: **use only the fields listed in section 2**. To show new data, confirm the field exists in the contract table first; if it is not listed, you cannot use it.
- Safety of a reference depends only on whether the field is listed, not on the current page type: on non-article pages `.ArticlePost` fields are zero values and are safe to reference (gate display with `.IsHome` / `.IsFolder`).

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

Put colors, spacing, and layout in `style.css`. Put page interactions in `theme.js`. `.ThemeConfigJS` comes from the linked plugin, not from the template.

`theme.json` is optional and declares capabilities plus public display fields:

```json
{
  "supports_public_blog": true,
  "public_settings": ["blog_name", "description", "logo_url", "logo_size", "logo_shape", "banner_url", "mobile_banner_url", "buttons"]
}
```

- `supports_public_blog`: must be `true` to serve `/b/<vault-id>` as a public blog home and to enable the public-blog switch in vault settings.
- `public_settings`: explicitly lists the blog-setting keys copied to the template-readable public config; keys not listed never reach the template.

Do not create `settings.json`. Templates do not own functional settings.

## 2. Template field contract (complete list)

These are all the fields a template may reference. Anything not in these tables is treated as nonexistent and triggers the fallback in section 0.

**2.1 Page and site**

| Field | Type | Notes |
| --- | --- | --- |
| `.Title` | string | Browser title |
| `.ThemeName` | string | Current theme name |
| `.ThemeBaseURL` | string | Theme asset base, e.g. `/themes/my-template`; always use it for bundled assets |
| `.ThemeConfigJS` | JS | Blog config JSON safe to embed in `<script>` |
| `.IsHome` | bool | Blog home page (`/b/<vault-id>`) |
| `.IsFolder` | bool | Folder index view |
| `.FolderTitle` | string | Folder title |
| `.ArticleTitle` | string | Article title (article pages) |
| `.ContentHTML` | HTML | Rendered article body or folder index |
| `.AllowCopy` | bool | Whether this share allows copy-to-clipboard |
| `.ShareID` | string | Current share ID |
| `.BlogHomeURL` | string | `/b/<vault-id>` when public blog is on, else empty |
| `.CustomHeader` / `.CustomFooter` | HTML | Vault custom header/footer fragments (may be empty) |
| `.FooterNotice` | HTML | Server footer notice |

**2.2 Blog identity and banner**

| Field | Type | Notes |
| --- | --- | --- |
| `.BlogName` | string | Blog name |
| `.Description` | string | Blog description |
| `.LogoURL` | string | Logo image URL |
| `.LogoSize` | int | Logo pixel size (`0` = unset) |
| `.LogoShape` | string | `square` or `circle` |
| `.BannerURL` | string | Page banner URL; fall back to a bundled image when empty |
| `.MobileBannerURL` | string | Mobile banner URL |
| `.Buttons` | list | Custom links; each has `.Label` / `.URL` / `.IconURL` |

Values come from blog settings (section 4) and are empty/zero when unset.

**2.3 Single-article metadata `.ArticlePost`**

Populated on article pages; zero-valued on home and folder-index pages (branch with `.IsHome` / `.IsFolder` to avoid showing empty info).

| Field | Type | Notes |
| --- | --- | --- |
| `.ArticlePost.Summary` | string | Summary; frontmatter `description`, else first body paragraph |
| `.ArticlePost.Date` | string | `YYYY-MM-DD`; frontmatter `published`, else file mtime |
| `.ArticlePost.Category` | string | Category (frontmatter `category`) |
| `.ArticlePost.Tags` | list | Tag strings (frontmatter `tags`) |
| `.ArticlePost.CoverURL` | string | Cover URL (frontmatter `image`, see section 3) |
| `.ArticlePost.WordCount` | int | Body word count (CJK per character, Latin per word) |
| `.ArticlePost.ReadingMinutes` | int | Estimated minutes (ceil at 400 words/min) |

**2.4 Home post list `.HomePosts`**

Available when `.IsHome` is true; each element has:

| Field | Type | Notes |
| --- | --- | --- |
| `.Title` | string | Post title |
| `.Summary` | string | Summary |
| `.URL` | string | Post URL `/p/<share-id>` |
| `.Date` | string | `YYYY-MM-DD` |
| `.Category` | string | Category |
| `.Tags` | list | Tags |
| `.CoverURL` | string | Cover URL |
| `.WordCount` | int | Word count |

## 3. Frontmatter metadata

An article's Markdown may start with a `---`-fenced YAML block that supplies the metadata above:

```markdown
---
title: A note
description: Summary for lists and search
published: 2026-09-22
category: Journal
tags: [note, life]
image: attachments/cover.webp
---
Body starts here.
```

- Every key is optional; when omitted, the title uses the filename, the summary uses the first body paragraph, and the date uses the file mtime.
- `tags` accepts an inline `[a, b]` array or an indented list (`- tag` per line).
- `image` is a vault attachment path or an `http(s)`/site-absolute URL; an attachment cover is resolved to a public URL and authorized for the share automatically, with no need to reference it again in the body.
- A fully closed `---` block is treated as metadata and hidden from the body; an unclosed or malformed block is kept as content and not parsed.

## 4. Where blog settings come from

`.BlogName`, `.Description`, `.LogoURL`, `.LogoSize`, `.LogoShape`, `.BannerURL`, `.MobileBannerURL`, and `.Buttons` are sourced two ways:

- **Built-in blog settings**: applies to any theme whose `theme.json` declares `supports_public_blog: true` (not only built-in themes). Open the top-level **Plugin settings**, pick the vault, and fill in name, description, logo, banner, and links.
- **Linked feature plugin**: a `plugin.zip` bundled with the template may declare extra settings; `theme.json` `public_settings` decides which keys reach the template-readable public config.

## 5. Link a feature plugin

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

## 6. Upload

From inside the template directory, run `zip -r ../my-template.zip .`, or use `Compress-Archive * ../my-template.zip` in PowerShell. Upload the ZIP from **Admin settings -> Templates**, or create a copy from an existing template. Names may contain letters, digits, hyphens, and underscores.

Templates provide presentation. Plugins provide functionality.
