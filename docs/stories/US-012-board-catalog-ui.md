# US-012: Browse ordered boards and compact catalogs from the local lineage

## Goal

Render the active lineage's boards and catalogs as a responsive, content-focused local browsing experience.

## User Value

The Reader can browse the observed board/catalog hierarchy quickly on mobile while always knowing which snapshot is being viewed and how stale it is.

## Scope

- Render boards and catalog pages/threads exclusively from the active IndexedDB lineage, preserving all upstream orders and page boundaries.
- Present board catalogs as responsive mobile-first compact rows.
- Keep lineage ULID and calculated snapshot age visible in all catalog/board states.
- Keep failed catalogs/resources in their original position with clear degraded presentation; show “Not available in this snapshot” for absent local resources.
- Provide board/thread selection with component state only; do not add History API application navigation, a client-side routing framework, or canonical 4Visor content URLs.
- Expose original canonical 4chan destinations where the snapshot supplies them.
- Use semantic, keyboard-operable controls and readable status text for basic accessibility.

## Out of Scope

- Thread post-body rendering, HTML sanitization, media loading, sorting/filtering/search, read state, bookmarks, recommendations, or resource fetch-on-selection.

## Dependencies

- US-010.

## Related MADRs

- None. View/history state and reply-selection mechanics are local frontend details under locked no-router and upstream-order constraints.

## Traceability

- `Full Requirements / Product` and `/ User interface` (`docs/SEED.md:86-97`, `159-167`): read-only behavior, canonical URLs, exact ordering, mobile-first compact catalogs, visible lineage/age, and degraded resources.
- `Operational Flows / Client rendering` and `/ Missing local resource` (`docs/SEED.md:896-944`): local-only rendering, no reorder/filter/fetch, degradation, and absent-resource message.
- `Design Notes / Client-first design`, `/ Upstream fidelity`, and `/ Honest degradation` (`docs/SEED.md:1323-1333`, `1374-1383`, `1394-1405`): IndexedDB serving, exact observation, and visible failures.
- `Locked Decisions / Product` and `/ Frontend` (`docs/SEED.md:2101-2114`, `2135-2151`): exclusions, Chrome target, responsive compact rows, and no router.

## Acceptance Criteria

1. Given a fixture lineage, rendered board, page, and thread-summary order exactly matches the source arrays, including failed entries in place.
2. Catalogs use compact responsive rows at mobile and wider viewport breakpoints supported by the chosen Tailwind design.
3. Lineage ID and age remain visible in board/catalog, empty, failed, and absent-resource views.
4. Selecting an absent item shows the required message and performs no backend/upstream fetch; failed items have a distinct visible degraded state.
5. No search, filter, ranking, recommendation, bookmark, or read-state control exists, and navigation uses no client-side router dependency.
6. As a planning-quality assumption rather than a SEED product feature, interactive controls are semantic, keyboard operable, and expose degraded/empty status in text rather than color alone.

## Validation

- Use Vitest component integration tests for order, page boundaries, responsive classes/layout states, visible lineage metadata, degraded/absent states, and zero fetches on selection.
- Unit-test snapshot-age formatting with a fake clock.
