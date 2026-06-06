# Scribe Agent Instructions

## Stack

- **Backend**: Go, `parchment` (graph store), `ordo` (rule engine)
- **MCP transport**: HTTP, stdio
- **Web UI**: Go templates + vanilla JS ES modules (no bundler)
- **Graph**: `3d-force-graph@1.80.0` (ForceGraph3D), `three@0.176.0` (UMD via CDN)
- **Frontend tests**: Vitest (unit), Playwright (cross-browser, Chrome + Firefox)
- **Go tests**: `go test ./...`, browser tests with `-tags browser`

## Critical constraints

- `nodeResolution` in `3d-force-graph` is a **scalar**, not a function. Passing a function produces `Math.floor(fn)=NaN` → zero-triangle sphere → invisible nodes.
- `window.THREE` must be `three@0.176.0` (UMD build). The bundled version (>=0.179) has Firefox rendering differences. The CDN URL is in `web/templates/graph.html`.
- `Graph.lights()` is a Kapsule prop setter. Calling `Graph.lights(arr)` triggers `update()` which recreates all node materials. Do not call it after `applyGraphData`.
- `nodeThreeObject` + external `window.THREE` causes two-instance problem — ForceGraph3D's bundled renderer rejects materials from a different THREE instance. Use built-in node rendering.

## Renderer

`web/static/graph/renderer.js` owns node appearance only: `nodeColor`, `nodeVal`, `nodeRelSize`, `nodeOpacity`. One method: `apply(graphBuilder)`. Everything else (physics, camera, data fetching, lifecycle) lives in `graph.js`.

To swap renderers, change one line in `graph.js`:
```js
renderer = new KindColorRenderer();   // hardcoded kind colors
renderer = new CSSVarRenderer();      // reads --graph-color-kind-* CSS vars
```

## Module layout

```
web/static/graph/
  graph.js          entry point — lifecycle, physics, camera, events
  graph-state.js    createGraphState() — all mutable runtime state
  renderer.js       BaseRenderer, KindColorRenderer, CSSVarRenderer
  logger.js         createLogger(name) — [name] prefix, level-controlled
  palette.js        buildPalette(culori, bgHex) — Oklch colour math
  physics.js        forceRadialSphere, equatorPriorityPositions, centerOfMass, …
  glow.js           glowColor, glowConfig — compliance glow meshes
  api.js            fetchScopeGraph, fetchKindGraph, fetchArtifactGraph, …
  ui.js             setModeBadge, openSidebar, showContextMenu, …
```

## Quality gate

```
cd web/static/graph && npm test              # Vitest unit tests
npx playwright test graph.spec.ts           # mock-backend, Chrome + Firefox
npx playwright test real.spec.ts            # real server (localhost:8083 must be up)
cd ../../../ && go test ./...               # Go unit tests
golangci-lint run --new-from-rev=HEAD       # lint
make restart                                # build image + redeploy
```

Run all before committing. `real.spec.ts` requires `make restart` first.

## Commits

`<type>: <what changed>` — imperative, lowercase, no period, ≤72 chars.
Types: `feat` `fix` `refactor` `test` `docs` `chore` `ci`

No tracker IDs in subject lines. No bullet lists of changed files.

## Comments

Zero by default. One line only for non-obvious WHY — external constraints, domain quirks, known pitfalls. The `// Critical constraints` section above is the right place for architectural reasoning, not inline code comments.
