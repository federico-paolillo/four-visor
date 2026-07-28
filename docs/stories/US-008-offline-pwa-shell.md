# US-008: Install and reopen the application shell offline

## Goal

Provide the Reader with a Chrome-for-Android-targeted Preact PWA shell that can be installed and reopened without a network connection.

## User Value

The Reader can launch 4Visor like an app and reach its local startup experience during server or network outages.

## Scope

- Build the shell with Preact, TypeScript, Tailwind CSS, native ES modules, and Vite for Chrome for Android 150+.
- Supply a Web App Manifest and Service Worker using browser APIs directly.
- Cache only versioned application-shell/static assets in Cache Storage; never snapshot JSON or media.
- Serve the cached shell offline and provide a deterministic shell-cache update/removal policy.
- Keep Preact as the only framework and avoid a state-management framework, client-side router, SSR, or multi-page architecture.
- Document supported browser, installation/offline-shell behavior, and cache boundaries.

## Out of Scope

- IndexedDB snapshot data, snapshot synchronization, content rendering, media caching, cross-browser compatibility, or an offline media promise.

## Dependencies

- US-002.

## Related MADRs

- None. Cache Storage boundaries and browser/framework choices are locked; shell precache/update mechanics and asset versioning are local implementation details.

## Traceability

- `Vision`, `Axioms`, and `Full Requirements / Product` (`docs/SEED.md:3-29`, `40-79`, `86-97`): read-only anonymous PWA, browser serving layer, Preact-only lifecycle, and no identity/personalization.
- `Full Requirements / Local storage` (`docs/SEED.md:128-139`): Cache Storage holds shell/static assets only.
- `Technology Stack / Frontend` and `/ Browser platform` (`docs/SEED.md:1779-1791`, `1833-1841`): selected frontend tools and browser APIs.
- `Technology Rationale / Preact`, `/ Tailwind CSS`, `/ Vite`, and `/ Service Worker` (`docs/SEED.md:1938-1975`, `2002-2013`): narrow framework/tooling and offline shell rationale.
- `Locked Decisions / Frontend` (`docs/SEED.md:2135-2151`): exact frontend stack, Chrome target, Service Worker boundary, and router exclusions.

## Acceptance Criteria

1. The production build produces a valid installable manifest and a Service Worker-controlled shell for Chrome for Android 150+.
2. After one successful shell load, an integration test can request the shell/static assets with network fetches failing and receive them from Cache Storage.
3. Cache Storage contains no snapshot response, IndexedDB data, or explicitly cached media.
4. Activating a new shell cache removes obsolete application-shell caches without clearing IndexedDB or browser-managed HTTP media cache.
5. The source/runtime dependency graph contains Preact but no additional UI/state/router/SSR framework.
6. The frontend validation tasks introduced by US-002 remain defined and all four mandated commands pass for this story.

## Validation

- Unit-test shell-cache allowlisting and version cleanup.
- Integration-test Service Worker install/activate/fetch behavior with browser API test doubles; do not create an end-to-end browser test.
