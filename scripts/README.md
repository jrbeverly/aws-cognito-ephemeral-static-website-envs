# Scripts

Build, packaging, and utility scripts for local development.

## Expected Scripts

- `build-example-sites.sh` — builds Hugo example sites and prepares artifacts
- `package-example-sites.sh` — packages built examples into uploadable zip files

## Expected Output

```text
dist/examples/hugo-basic.zip
dist/examples/hugo-docs.zip
dist/examples/single-index.html
```

These artifacts are used as test inputs for the portal upload flows.

See `VISION.md` §10.4 for script requirements.
