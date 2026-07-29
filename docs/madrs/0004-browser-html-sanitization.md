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

### Sanitization policy

DOMPurify parses each post into a detached HTML `DocumentFragment` with content
preservation enabled. The element allowlist is `a`, `b`, `br`, `code`, `em`,
`i`, `pre`, `s`, `span`, `strong`, `u`, and `wbr`. Forms, objects, SVG, MathML,
scripts, styles, templates, frames, and other active or embedded content are not
supported.

Only `href` and `class` may enter the detached fragment. Data, ARIA, event,
style, source, target, ID, and unknown attributes are disabled. `class` is an
intermediate discriminator only: `quotelink` is recognized on `a`, `quote` and
`deadlink` on `span`, and `prettyprint` on `pre`. Before serialization every
upstream attribute is removed. Recognized semantics receive fixed
application-owned classes, and safe anchors receive only a validated `href`
plus a fixed application-owned class. No upstream class or style reaches the
main document.

Unsupported inert elements are unwrapped so their visible descendant text and
independently supported descendants remain. Active content is removed with its
contents. An unsafe or missing link is unwrapped so its visible label remains
as non-clickable content. Malformed input is repaired by detached browser
parsing before the same policy is applied.

Only HTTP and HTTPS destinations survive. Relative destinations require and
resolve against the containing post's validated canonical 4chan thread, not the
PWA origin; without valid context they become non-clickable text.
Recognized `quotelink` fragments, 4chan or 4channel thread paths, and legacy
`res` paths are rebuilt from validated lowercase board and positive safe-integer
thread/post coordinates as exact HTTPS
`boards.4chan.org/{board}/thread/{thread}[#p{post}]` destinations. Other safe
HTTP(S) links retain their resolved destination. No link uses application
routing, History API state, or an application click handler.

### Security invariants

- Raw upstream HTML is accepted only by the `PostMarkup` boundary.
- The sole raw-HTML rendering sink is private to that component and receives a
  branded value returned by the complete sanitizer and normalization pipeline.
- No caller can provide a pre-sanitized bypass value to the rendering sink.
- Post-sanitization mutation is limited to removing attributes, assigning fixed
  application classes, and assigning validated URLs.
- Sanitizer exceptions fail closed: Preact receives the original string only as
  an ordinary escaped text child.
- Expanding the allowlists or adding another raw-HTML sink requires a security
  review and focused regression tests.

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
