# Sanitize upstream HTML with a proven browser-side allowlist sanitizer

## Context

Post HTML is intentionally stored unchanged and is untrusted. The PWA must
preserve supported markup and safe links, turn unsupported markup into text,
canonicalize quote navigation to 4chan, and never place unsanitized HTML in the
main document. This security boundary is consequential enough that a bespoke
parser is not the minimal safe choice.

## Decision

Use DOMPurify in the frontend with an explicit, minimal element/attribute
allowlist for supported 4chan post markup. Preserve the textual content of
unsupported elements while removing their element semantics. Remove event
handlers, styling injection, active content, and unsafe URL schemes. After
sanitization, canonicalize recognized quote links to HTTPS 4chan thread/post
URLs using the containing board/thread context; retain other safe HTTP(S)
destinations. Render only the resulting sanitized value.

The backend continues storing the upstream HTML string unchanged. Unsafe links
degrade to non-clickable text; the requirement to keep hyperlinks clickable
does not override the trust-boundary requirement.

## Decision Drivers

- Treat upstream HTML as hostile at the browser boundary.
- Avoid maintaining a custom HTML sanitizer.
- Preserve safe supported markup and visible text.
- Keep canonical navigation outside 4Visor.
- Leave the backend representation faithful to upstream data.
- Fit the Chrome-for-Android-only target without relying on an immature browser
  Sanitizer API contract.

## Considered Options

1. **DOMPurify with an explicit allowlist — chosen.** Mature and narrowly scoped;
   custom logic is limited to 4chan markup and quote-link policy.
2. **Custom `DOMParser` tree walker.** Avoids a dependency but makes the project
   responsible for subtle element, attribute, URL, namespace, and mutation-XSS
   behavior.
3. **Native Sanitizer API.** Attractive platform ownership, but its exact
   contract is not selected by the seed and would make the security boundary
   depend on browser support details outside the product specification.
4. **Sanitize in the backend.** Violates unchanged backend storage, shifts the
   trust boundary, and risks conflating cache fidelity with presentation.
5. **Render raw HTML.** Explicitly forbidden and unsafe.

## Consequences

### Positive

- The riskiest content boundary uses a maintained security-focused component.
- Backend snapshots remain faithful and reusable as specified.
- Safe link and unsupported-markup behavior can be tested independently from UI
  layout.
- No custom general-purpose HTML parser/sanitizer is introduced.

### Negative

- DOMPurify becomes an additional frontend dependency that must be updated.
- The allowlist and quote-link transformation remain project-owned policy.
- Malicious or unsupported link schemes cannot remain clickable.
- Sanitizer output may not reproduce every visual detail of upstream HTML.

## Related User Stories

- [US-013](../stories/US-013-safe-post-markup.md)
- [US-014](../stories/US-014-thread-reader.md)

## Traceability

- `Axioms`: original post HTML is stored unchanged; Preact/browser ownership.
- `Full Requirements / Rendering`.
- `High-Level Architecture / Snapshot contents`.
- `Operational Flows / Client rendering` and `Post markup rendering`.
- `Design Notes / Upstream fidelity` and `Browser platform first`.
- `Locked Decisions / Rendering`.
- `Technology Stack / Frontend` and `Data formats`.
