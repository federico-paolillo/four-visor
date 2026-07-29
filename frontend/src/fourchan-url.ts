// This module builds canonical 4chan destinations from validated snapshot coordinates.

import type { OpaqueObject } from "./snapshot";

export const canonical4chanOrigin = "https://boards.4chan.org";
export const canonical4chanMediaOrigin = "https://i.4cdn.org";

const boardIdentifierPattern = /^[a-z0-9]+$/;
const semanticPathPattern = /^[a-z0-9-]+$/;
const mediaExtensionPattern = /^\.[a-z0-9]+$/;
const imageExtensions = new Set([".gif", ".jpg", ".png"]);
const videoExtensions = new Set([".mp4", ".webm"]);
const audioExtensions = new Set([".flac", ".mp3", ".ogg", ".wav"]);

export type CanonicalPostMedia = {
  readonly filename: string;
  readonly fullURL: string;
  readonly height: number;
  readonly kind: "audio" | "file" | "image" | "video";
  readonly spoiler: boolean;
  readonly thumbnailHeight: number;
  readonly thumbnailURL: string;
  readonly thumbnailWidth: number;
  readonly width: number;
};

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

// canonicalPostMedia derives original 4cdn URLs only from a complete validated attachment shape.
export function canonicalPostMedia(
  board: OpaqueObject,
  post: OpaqueObject,
): CanonicalPostMedia | undefined {
  const identifier = textField(board, "board");
  const timestamp = positiveInteger(fieldValue(post, "tim"));
  const extension = textField(post, "ext");
  const filename = textField(post, "filename");
  const width = positiveInteger(fieldValue(post, "w"));
  const height = positiveInteger(fieldValue(post, "h"));
  const thumbnailWidth = positiveInteger(fieldValue(post, "tn_w"));
  const thumbnailHeight = positiveInteger(fieldValue(post, "tn_h"));
  const spoilerField = Object.getOwnPropertyDescriptor(post, "spoiler");
  if (
    identifier === undefined ||
    !boardIdentifierPattern.test(identifier) ||
    timestamp === undefined ||
    extension === undefined ||
    !mediaExtensionPattern.test(extension) ||
    filename === undefined ||
    width === undefined ||
    height === undefined ||
    thumbnailWidth === undefined ||
    thumbnailHeight === undefined ||
    (spoilerField !== undefined && spoilerField.value !== 1)
  ) {
    return undefined;
  }

  return {
    filename: `${filename}${extension}`,
    fullURL: canonicalMediaURL(`/${identifier}/${timestamp}${extension}`),
    height,
    kind: mediaKind(extension),
    spoiler: spoilerField !== undefined,
    thumbnailHeight,
    thumbnailURL: canonicalMediaURL(`/${identifier}/${timestamp}s.jpg`),
    thumbnailWidth,
    width,
  };
}

function textField(object: OpaqueObject, field: string): string | undefined {
  const value = fieldValue(object, field);
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function fieldValue(object: OpaqueObject, field: string): unknown {
  return Object.getOwnPropertyDescriptor(object, field)?.value;
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

function canonicalMediaURL(path: string): string {
  return new URL(path, canonical4chanMediaOrigin).href;
}

function mediaKind(extension: string): "audio" | "file" | "image" | "video" {
  if (imageExtensions.has(extension)) {
    return "image";
  }
  if (videoExtensions.has(extension)) {
    return "video";
  }
  return audioExtensions.has(extension) ? "audio" : "file";
}
