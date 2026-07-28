# US-013: Render upstream post markup safely with canonical links

## Goal

Convert unchanged upstream HTML into safe Preact-renderable content while retaining supported meaning and making unsupported markup visible as text.

## User Value

The Reader can read formatted posts and follow legitimate links without upstream HTML gaining unsafe access to the main document.

## Scope

- Parse upstream post HTML in an isolated DOM representation and apply a strict documented element/attribute/protocol allowlist before rendering.
- Render supported safe markup; convert unsupported elements/attributes to visible plain text rather than silently dropping their textual content.
- Keep external links clickable at their original safe destination.
- Resolve quote links to canonical 4chan thread/post URLs, never internal PWA navigation.
- Ensure no code path injects unsanitized upstream HTML into the main document.
- Document the allowlist, URL/protocol policy, unsupported fallback, and security invariants.

## Out of Scope

- Rewriting upstream HTML in the backend/cache, inline navigation, link previews, content moderation, media proxying, or sanitizing trusted application templates.

## Dependencies

- US-008.

## Related MADRs

- [MADR 0004](../madrs/0004-browser-html-sanitization.md), “Sanitize upstream HTML with a proven browser-side allowlist sanitizer,” including the frontend trust boundary, explicit allowlist, safe-link policy, and visible-text fallback.

## Traceability

- `Full Requirements / Rendering` (`docs/SEED.md:140-148`): unchanged backend HTML, frontend sanitization, text fallback, external links, canonical quote links, and no unsafe injection.
- `Operational Flows / Post markup rendering` (`docs/SEED.md:945-965`): isolated parse, strict allowlist, text fallback, and original link destinations.
- `Design Notes / Upstream fidelity` (`docs/SEED.md:1374-1383`): original HTML is preserved by the backend.
- `Locked Decisions / Backend cache` and `/ Rendering` (`docs/SEED.md:2126-2134`, `2152-2159`): unchanged storage and mandatory safe frontend rendering.

## Acceptance Criteria

1. Representative supported 4chan markup retains intended text/formatting after sanitization.
2. Scripts, event handlers, unsafe URL protocols, executable/embedded content, style-based escape vectors, and unknown dangerous attributes cannot enter the rendered main document.
3. Unsupported markup's user-visible text remains visible as plain text rather than disappearing or executing.
4. Safe external links remain clickable at the same destination; quote links resolve to canonical 4chan URLs and do not invoke internal routing.
5. Rendering APIs receive only sanitizer output, with no alternate raw-HTML path.

## Validation

- Table-driven Vitest unit tests cover supported markup, nested unsupported markup, malformed HTML, XSS payload classes, protocol handling, external URLs, and quote URLs.
- Component integration-test that only sanitized nodes reach the post renderer.
