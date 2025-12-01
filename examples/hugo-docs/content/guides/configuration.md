---
title: "Configuration"
---

# Configuration

This page describes how to create and configure a Hugo project.

## Create a new site

```sh
hugo new site my-site
cd my-site
```

## Understanding hugo.toml

The `hugo.toml` file controls site-wide settings:

```toml
baseURL = "https://example.com/"
languageCode = "en-us"
title = "My Site"
```

## Add a theme

Hugo has many community themes. To use one:

```sh
git init
git submodule add https://github.com/theNewDynamic/gohugo-theme-ananke themes/ananke
echo "theme = 'ananke'" >> hugo.toml
```

Or build a custom layout from scratch with the `layouts/` directory.

## Create your first page

```sh
hugo new posts/my-first-post.md
```

Edit `content/posts/my-first-post.md` and add your content, then build:

```sh
hugo
```

The generated site appears in the `public/` directory.

## Next steps

Move on to [Deployment](/guides/deployment/) to learn how to package and publish
your site.

[← Back to Guides](/guides/)
