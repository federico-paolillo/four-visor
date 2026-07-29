// This module builds canonical 4chan destinations from validated snapshot coordinates.

import type { OpaqueObject } from "./snapshot";

export const canonical4chanOrigin = "https://boards.4chan.org";

const boardIdentifierPattern = /^[a-z0-9]+$/;
const semanticPathPattern = /^[a-z0-9-]+$/;

// canonicalBoardURL accepts only a board belonging to the canonical HTTPS origin.
export function canonicalBoardURL(board: OpaqueObject): string | undefined {
  const identifier = textField(board, "board");
  return identifier !== undefined && boardIdentifierPattern.test(identifier)
    ? canonicalURL(`/${identifier}/`)
    : undefined;
}

// canonicalThreadURL accepts only a valid board, thread number, and optional safe slug.
export function canonicalThreadURL(
  board: OpaqueObject,
  summary: OpaqueObject,
): string | undefined {
  const identifier = textField(board, "board");
  const number = positiveInteger(summary.no);
  if (
    identifier === undefined ||
    !boardIdentifierPattern.test(identifier) ||
    number === undefined
  ) {
    return undefined;
  }

  const semanticPath = textField(summary, "semantic_url");
  const suffix =
    semanticPath !== undefined && semanticPathPattern.test(semanticPath)
      ? `/${semanticPath}`
      : "";
  return canonicalURL(`/${identifier}/thread/${number}${suffix}`);
}

// canonicalPostURL builds an exact canonical thread or post URL from scalar coordinates.
export function canonicalPostURL(
  board: unknown,
  thread: unknown,
  post?: unknown,
): string | undefined {
  const threadNumber = positiveInteger(thread);
  const postNumber = post === undefined ? undefined : positiveInteger(post);
  if (
    typeof board !== "string" ||
    !boardIdentifierPattern.test(board) ||
    threadNumber === undefined ||
    (post !== undefined && postNumber === undefined)
  ) {
    return undefined;
  }

  const fragment = postNumber === undefined ? "" : `#p${postNumber}`;
  return canonicalURL(`/${board}/thread/${threadNumber}${fragment}`);
}

function textField(object: OpaqueObject, field: string): string | undefined {
  const value = object[field];
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function positiveInteger(value: unknown): number | undefined {
  return Number.isSafeInteger(value) && Number(value) > 0
    ? Number(value)
    : undefined;
}

function canonicalURL(path: string): string | undefined {
  const url = new URL(path, canonical4chanOrigin);
  return url.protocol === "https:" && url.origin === canonical4chanOrigin
    ? url.href
    : undefined;
}
