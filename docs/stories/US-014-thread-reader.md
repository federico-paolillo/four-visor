# US-014: Read nested, collapsible threads with honest degradation

## Goal

Render a selected thread's ordered posts in a compact nested and collapsible local view, including failed, absent, and oversized outcomes.

## User Value

The Reader can follow a conversation comfortably while seeing exactly where the frozen snapshot is incomplete or truncated.

## Scope

- Render thread posts from the active local lineage in upstream order using the sanitized markup boundary.
- Derive and display visual reply nesting from available upstream reply/quote relationships without reordering posts.
- Make posts collapsible with accessible native/Preact controls and no persisted read/preference state.
- Keep lineage ID and age visible in the thread view.
- Render `failed` threads in place, `oversize` threads with the first 250 posts plus a clear truncation notice, and absent threads as “Not available in this snapshot.”
- Offer canonical 4chan thread/post links for unavailable content or quotes; never fetch missing/additional posts.

## Out of Scope

- Posts after the first 250, inline reply navigation as application routing, posting/replying/moderation, read tracking, saved collapse state, search, filtering, or backend reconciliation.

## Dependencies

- US-012.
- US-013.

## Related MADRs

- None. Reply nesting is presentation-only and must retain post sequence; the exact parent/indentation heuristic is a local UI choice.

## Traceability

- `Full Requirements / User interface` (`docs/SEED.md:159-167`): responsive layout, nested replies, collapsible posts, visible lineage/age, and degraded resources.
- `Operational Flows / Client rendering` and `/ Missing local resource` (`docs/SEED.md:896-944`): local-only ordered content, visible failed/oversize resources, and absent message.
- `Failure Semantics / Failure matrix` (`docs/SEED.md:1735-1749`): oversize first 250 and missing-resource outcomes.
- `Locked Decisions / Frontend` and `/ Rendering` (`docs/SEED.md:2135-2159`): nesting, collapse, no router, sanitization, canonical links, and degradation.

## Acceptance Criteria

1. Posts appear in the exact snapshot order; visual nesting changes presentation only and cannot reorder/filter content.
2. Every post can be expanded/collapsed with a semantic keyboard-operable control, and collapse state is not persisted as preference/read state.
3. `oversize` renders exactly the stored first 250 posts and an explicit truncation message; `failed` and absent states remain visibly distinct and in context.
4. Lineage ID/age remain visible and every post body is rendered through US-013 sanitation.
5. Selecting missing/additional content makes zero backend requests and offers only the applicable canonical external 4chan destination.

## Validation

- Vitest component integration tests cover order-preserving nesting, collapse interaction, sanitizer use, lineage metadata, present/failed/oversize/absent states, canonical links, accessibility roles, and zero resource fetches.
