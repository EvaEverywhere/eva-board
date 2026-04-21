# Eva Board docs site

[Docusaurus 3](https://docusaurus.io/) site that renders the markdown
under the repo-root [`docs/`](../docs) folder. Deployed to
<https://evaeverywhere.github.io/eva-board/> via the
[`docs-deploy.yml`](../.github/workflows/docs-deploy.yml) GitHub Action
on every push to `main` that touches `docs/`, `website/`, or the
workflow itself.

The Markdown source lives in `../docs` (Docusaurus reads it via
`path: '../docs'` in `docusaurus.config.ts`). Editing a file in
`docs/` updates both the terminal-readable copy and the website on the
next deploy.

## Local development

```bash
npm install
npm run start
# Open http://localhost:3000/eva-board/
```

`npm run start` watches both `docs/` and `website/` for changes and
hot-reloads the page.

## Build

```bash
npm run build
# Output in ./build, served by the GitHub Action.
```

`npm run serve` serves the production build locally for a final smoke
test.

## Sidebar + ordering

Sidebar layout is hand-curated in [`sidebars.ts`](./sidebars.ts).
`sidebar_position` frontmatter is included on each doc as a fallback for
auto-generated sidebars but is currently unused.

## One-time GitHub setup

Repo Settings → Pages → Source must be set to **GitHub Actions** for
the deploy workflow to publish. The workflow will fail until that's
enabled.
