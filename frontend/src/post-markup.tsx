// This module is the sole browser boundary between hostile post HTML and Preact.

import DOMPurify from "dompurify";

import { canonicalPostURL } from "./fourchan-url";

const allowedElements = [
  "a",
  "b",
  "br",
  "code",
  "em",
  "i",
  "pre",
  "s",
  "span",
  "strong",
  "u",
  "wbr",
] as const;
const activeContentElements = [
  "form",
  "iframe",
  "math",
  "noscript",
  "object",
  "script",
  "style",
  "svg",
  "template",
] as const;
const quoteHosts = new Set(["boards.4chan.org", "boards.4channel.org"]);
const quotePathPattern =
  /^\/([a-z0-9]+)\/(?:thread|res)\/([1-9][0-9]*)(?:\/[a-z0-9-]+)?\/?$/;
const quotePostPattern = /^#p([1-9][0-9]*)$/;
const linkClass =
  "text-cyan-300 underline decoration-cyan-700 underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400";
const quoteClass = "text-emerald-400";
const deadLinkClass = "text-slate-500 line-through";
const codeClass =
  "my-2 overflow-x-auto rounded bg-slate-950 p-2 font-mono text-sm";

declare const sanitizedPostMarkupBrand: unique symbol;
type SanitizedPostMarkup = string & {
  readonly [sanitizedPostMarkupBrand]: true;
};

export type PostMarkupContext = {
  readonly board: string;
  readonly thread: number;
};

// sanitizePostMarkup returns only serialized nodes that passed the complete policy.
export function sanitizePostMarkup(
  rawHTML: string,
  context: PostMarkupContext,
): SanitizedPostMarkup {
  const fragment = DOMPurify.sanitize(rawHTML, {
    ALLOWED_ATTR: ["class", "href"],
    ALLOWED_NAMESPACES: ["http://www.w3.org/1999/xhtml"],
    ALLOWED_TAGS: [...allowedElements],
    ALLOW_ARIA_ATTR: false,
    ALLOW_DATA_ATTR: false,
    ADD_FORBID_CONTENTS: [...activeContentElements],
    KEEP_CONTENT: true,
    RETURN_DOM_FRAGMENT: true,
  });

  normalizeElements(fragment, context);
  const template = document.createElement("template");
  template.content.append(fragment);
  return template.innerHTML as SanitizedPostMarkup;
}

// PostMarkup renders raw input only after sanitization and fails closed to Preact text.
export function PostMarkup({
  html,
  board,
  thread,
}: {
  readonly html: string;
  readonly board: string;
  readonly thread: number;
}) {
  const className = "break-words text-sm leading-6 text-slate-200";
  try {
    const sanitized = sanitizePostMarkup(html, { board, thread });
    return (
      <div class={className} dangerouslySetInnerHTML={{ __html: sanitized }} />
    );
  } catch {
    return <div class={className}>{html}</div>;
  }
}

function normalizeElements(
  fragment: DocumentFragment,
  context: PostMarkupContext,
): void {
  for (const element of fragment.querySelectorAll("*")) {
    const sourceClasses = new Set(element.classList);
    const sourceHref = element.getAttribute("href");
    for (const attribute of element.getAttributeNames()) {
      element.removeAttribute(attribute);
    }

    switch (element.localName) {
      case "a":
        normalizeAnchor(
          element,
          sourceHref,
          sourceClasses.has("quotelink"),
          context,
        );
        break;
      case "pre":
        if (sourceClasses.has("prettyprint")) {
          element.setAttribute("class", codeClass);
        }
        break;
      case "span": {
        const classes = [
          sourceClasses.has("quote") ? quoteClass : "",
          sourceClasses.has("deadlink") ? deadLinkClass : "",
        ].filter(Boolean);
        if (classes.length > 0) {
          element.setAttribute("class", classes.join(" "));
        }
        break;
      }
    }
  }
}

function normalizeAnchor(
  anchor: Element,
  sourceHref: string | null,
  quote: boolean,
  context: PostMarkupContext,
): void {
  const href =
    sourceHref === null || sourceHref.trim() === ""
      ? undefined
      : safeDestination(sourceHref, quote, context);
  if (href === undefined) {
    anchor.replaceWith(...anchor.childNodes);
    return;
  }

  anchor.setAttribute("class", linkClass);
  anchor.setAttribute("href", href);
}

function safeDestination(
  href: string,
  quote: boolean,
  context: PostMarkupContext,
): string | undefined {
  if (quote) {
    const sameThreadPost = quotePostPattern.exec(href);
    if (sameThreadPost !== null) {
      return canonicalPostURL(
        context.board,
        context.thread,
        Number(sameThreadPost[1]),
      );
    }
  }

  let destination: URL;
  const base = canonicalPostURL(context.board, context.thread);
  try {
    destination = base === undefined ? new URL(href) : new URL(href, base);
  } catch {
    return undefined;
  }
  if (destination.protocol !== "http:" && destination.protocol !== "https:") {
    return undefined;
  }

  const canonicalQuote = quote ? canonicalQuoteURL(destination) : undefined;
  return canonicalQuote ?? destination.href;
}

function canonicalQuoteURL(destination: URL): string | undefined {
  if (
    !quoteHosts.has(destination.hostname) ||
    destination.port !== "" ||
    destination.username !== "" ||
    destination.password !== ""
  ) {
    return undefined;
  }

  const path = quotePathPattern.exec(destination.pathname);
  if (path === null) {
    return undefined;
  }
  const post = quotePostPattern.exec(destination.hash);
  return post === null
    ? canonicalPostURL(path[1], Number(path[2]))
    : canonicalPostURL(path[1], Number(path[2]), Number(post[1]));
}
