---
title: "Deployment"
---

# Deployment

This page describes how to build, package, and upload a Hugo site through the
portal.

## Build the site

From your Hugo project directory:

```sh
hugo
```

Hugo writes the complete static site to `public/`.  Every page, asset, and
layout file is rendered into plain HTML, CSS, and JavaScript.

## Package the output

Create a zip archive of the `public/` directory:

```sh
cd public
zip -r ../my-site.zip .
cd ..
```

Make sure `index.html` is at the root of the zip — this is required by the
platform validator.

## Upload through the portal

1. Log in to the management portal.
2. Create or select a site namespace.
3. Upload `my-site.zip`.
4. Wait for validation to complete.
5. If validation passes, the site is published automatically.

## Verify the published site

After publishing, open the hosted URL shown in the portal.  Confirm that:

- Every page loads correctly.
- Navigation links between pages work.
- CSS and static assets are served with the correct content type.
- Nested paths resolve to the correct pages (e.g., `/guides/deployment/`).

[← Back to Guides](/guides/)
