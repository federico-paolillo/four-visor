// This module models and validates the exact snapshot version 1 browser boundary.

export type OpaqueObject = Record<string, unknown>;

export type Boards =
  | { readonly state: "failed" }
  | { readonly state: "present"; readonly items: readonly BoardItem[] };

export type BoardItem = {
  readonly board: OpaqueObject;
  readonly catalog?: Catalog;
};

export type Catalog =
  | { readonly state: "failed" }
  | { readonly state: "present"; readonly pages: readonly Page[] };

export type Page = {
  readonly metadata: OpaqueObject;
  readonly threads: readonly ThreadEntry[];
};

export type ThreadEntry = {
  readonly summary: OpaqueObject;
  readonly thread?: Thread;
};

export type Thread =
  | { readonly state: "failed" }
  | { readonly state: "present"; readonly posts: readonly OpaqueObject[] }
  | { readonly state: "oversize"; readonly posts: readonly OpaqueObject[] };

export type SnapshotV1 = {
  readonly schemaVersion: 1;
  readonly lineageId: string;
  readonly observedAt: string;
  readonly boards: Boards;
};

export type SnapshotErrorKind =
  | "invalid-json"
  | "invalid-contract"
  | "unsupported-version";

const maximumCatalogThreads = 250;
const maximumThreadPosts = 250;
const ulidPattern = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/i;
const utcTimestampPattern =
  /^([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(?:\.[0-9]+)?(?:Z|\+00:00)$/;

// SnapshotError classifies a boundary failure without echoing untrusted values.
export class SnapshotError extends Error {
  readonly cause: unknown;
  readonly kind: SnapshotErrorKind;

  constructor(
    kind: SnapshotErrorKind,
    path: string,
    problem: string,
    cause?: unknown,
  ) {
    super(`${kind} at ${path}: ${problem}`);
    this.name = "SnapshotError";
    this.kind = kind;
    this.cause = cause;
  }
}

// parseSnapshot parses and validates one complete JSON snapshot document.
export function parseSnapshot(text: string): SnapshotV1 {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch (cause) {
    throw failure("invalid-json", "snapshot", "must be valid JSON", cause);
  }

  return validateSnapshot(value);
}

// validateSnapshot validates an already parsed value without cloning or normalizing it.
export function validateSnapshot(value: unknown): SnapshotV1 {
  const root = exactObject(value, "snapshot", [
    "schemaVersion",
    "lineageId",
    "observedAt",
    "boards",
  ]);

  if (
    typeof root.schemaVersion !== "number" ||
    !Number.isInteger(root.schemaVersion)
  ) {
    throw failure(
      "invalid-contract",
      "snapshot.schemaVersion",
      "must be an integer",
    );
  }
  if (root.schemaVersion !== 1) {
    throw failure(
      "unsupported-version",
      "snapshot.schemaVersion",
      "must equal 1",
    );
  }

  const lineageId = stringValue(root.lineageId, "snapshot.lineageId");
  if (!ulidPattern.test(lineageId)) {
    throw failure(
      "invalid-contract",
      "snapshot.lineageId",
      "must be a valid ULID",
    );
  }

  const observedAt = stringValue(root.observedAt, "snapshot.observedAt");
  if (!validObservedAt(observedAt)) {
    throw failure(
      "invalid-contract",
      "snapshot.observedAt",
      "must be a UTC RFC 3339 timestamp",
    );
  }

  validateBoards(root.boards, "snapshot.boards");

  return value as SnapshotV1;
}

function validateBoards(value: unknown, path: string): void {
  const boards = exactObject(value, path, ["state"], ["items"]);
  const state = stringValue(boards.state, `${path}.state`);
  const hasItems = hasOwn(boards, "items");

  if (state === "failed") {
    if (hasItems) {
      throw failure(
        "invalid-contract",
        path,
        "failed state must not contain items",
      );
    }
    return;
  }
  if (state !== "present") {
    throw failure(
      "invalid-contract",
      `${path}.state`,
      "must be failed or present",
    );
  }
  if (!hasItems) {
    throw failure(
      "invalid-contract",
      `${path}.items`,
      "is required for present state",
    );
  }

  arrayValue(boards.items, `${path}.items`).forEach((item, index) => {
    validateBoardItem(item, `${path}.items[${index}]`);
  });
}

function validateBoardItem(value: unknown, path: string): void {
  const item = exactObject(value, path, ["board"], ["catalog"]);
  opaqueObject(item.board, `${path}.board`);
  if (hasOwn(item, "catalog")) {
    validateCatalog(item.catalog, `${path}.catalog`);
  }
}

function validateCatalog(value: unknown, path: string): void {
  const catalog = exactObject(value, path, ["state"], ["pages"]);
  const state = stringValue(catalog.state, `${path}.state`);
  const hasPages = hasOwn(catalog, "pages");

  if (state === "failed") {
    if (hasPages) {
      throw failure(
        "invalid-contract",
        path,
        "failed state must not contain pages",
      );
    }
    return;
  }
  if (state !== "present") {
    throw failure(
      "invalid-contract",
      `${path}.state`,
      "must be failed or present",
    );
  }
  if (!hasPages) {
    throw failure(
      "invalid-contract",
      `${path}.pages`,
      "is required for present state",
    );
  }

  let threadCount = 0;
  arrayValue(catalog.pages, `${path}.pages`).forEach((page, index) => {
    threadCount += validatePage(page, `${path}.pages[${index}]`);
    if (threadCount > maximumCatalogThreads) {
      throw failure(
        "invalid-contract",
        `${path}.pages`,
        "must contain at most 250 threads",
      );
    }
  });
}

function validatePage(value: unknown, path: string): number {
  const page = exactObject(value, path, ["metadata", "threads"]);
  opaqueObject(page.metadata, `${path}.metadata`);
  const threads = arrayValue(page.threads, `${path}.threads`);
  threads.forEach((thread, index) => {
    validateThreadEntry(thread, `${path}.threads[${index}]`);
  });

  return threads.length;
}

function validateThreadEntry(value: unknown, path: string): void {
  const entry = exactObject(value, path, ["summary"], ["thread"]);
  opaqueObject(entry.summary, `${path}.summary`);
  if (hasOwn(entry, "thread")) {
    validateThread(entry.thread, `${path}.thread`);
  }
}

function validateThread(value: unknown, path: string): void {
  const thread = exactObject(value, path, ["state"], ["posts"]);
  const state = stringValue(thread.state, `${path}.state`);
  const hasPosts = hasOwn(thread, "posts");

  if (state === "failed") {
    if (hasPosts) {
      throw failure(
        "invalid-contract",
        path,
        "failed state must not contain posts",
      );
    }
    return;
  }
  if (state !== "present" && state !== "oversize") {
    throw failure(
      "invalid-contract",
      `${path}.state`,
      "must be failed, present, or oversize",
    );
  }
  if (!hasPosts) {
    throw failure(
      "invalid-contract",
      `${path}.posts`,
      "is required for present and oversize states",
    );
  }

  const posts = arrayValue(thread.posts, `${path}.posts`);
  if (state === "present" && posts.length > maximumThreadPosts) {
    throw failure(
      "invalid-contract",
      `${path}.posts`,
      "present state must contain at most 250 posts",
    );
  }
  if (state === "oversize" && posts.length !== maximumThreadPosts) {
    throw failure(
      "invalid-contract",
      `${path}.posts`,
      "oversize state must contain exactly 250 posts",
    );
  }
  posts.forEach((post, index) => {
    opaqueObject(post, `${path}.posts[${index}]`);
  });
}

function exactObject(
  value: unknown,
  path: string,
  required: readonly string[],
  optional: readonly string[] = [],
): Record<string, unknown> {
  const object = opaqueObject(value, path);
  const allowed = new Set([...required, ...optional]);

  for (const field of required) {
    if (!hasOwn(object, field)) {
      throw failure("invalid-contract", `${path}.${field}`, "is required");
    }
  }
  if (Object.keys(object).some((field) => !allowed.has(field))) {
    throw failure("invalid-contract", path, "contains an unknown field");
  }

  return object;
}

function opaqueObject(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw failure("invalid-contract", path, "must be an opaque object");
  }

  return value as Record<string, unknown>;
}

function arrayValue(value: unknown, path: string): readonly unknown[] {
  if (!Array.isArray(value)) {
    throw failure("invalid-contract", path, "must be an array");
  }

  return value;
}

function stringValue(value: unknown, path: string): string {
  if (typeof value !== "string") {
    throw failure("invalid-contract", path, "must be a string");
  }

  return value;
}

function validObservedAt(value: string): boolean {
  const match = utcTimestampPattern.exec(value);
  if (match === null) {
    return false;
  }

  const instant = new Date(`${match[1]}Z`);

  return (
    !Number.isNaN(instant.valueOf()) &&
    instant.toISOString().slice(0, 19) === match[1]
  );
}

function hasOwn(object: Record<string, unknown>, field: string): boolean {
  return Object.getOwnPropertyDescriptor(object, field) !== undefined;
}

function failure(
  kind: SnapshotErrorKind,
  path: string,
  problem: string,
  cause?: unknown,
): SnapshotError {
  return new SnapshotError(kind, path, problem, cause);
}
