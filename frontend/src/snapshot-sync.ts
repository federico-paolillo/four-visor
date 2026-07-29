// This module performs one due snapshot request and classifies its replacement outcome.

import {
  SnapshotError,
  type SnapshotErrorKind,
  type SnapshotV1,
} from "./snapshot";
import {
  replaceActiveSnapshot,
  SnapshotReplacementError,
  type SnapshotReplacementErrorKind,
} from "./snapshot-storage";

export type SnapshotSynchronizationErrorKind =
  | "network"
  | "gone"
  | "http"
  | SnapshotErrorKind
  | SnapshotReplacementErrorKind;

// SnapshotSynchronizationError keeps boundary causes behind safe classifications.
export class SnapshotSynchronizationError extends Error {
  readonly cause: unknown;
  readonly kind: SnapshotSynchronizationErrorKind;
  readonly status?: number;

  constructor(
    kind: SnapshotSynchronizationErrorKind,
    cause?: unknown,
    status?: number,
  ) {
    super("snapshot synchronization failed");
    this.name = "SnapshotSynchronizationError";
    this.kind = kind;
    this.cause = cause;
    this.status = status;
  }
}

export type SnapshotFetch = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>;

export type SnapshotReplacement = (
  serialized: string,
  signal: AbortSignal,
) => Promise<SnapshotV1>;

// synchronizeSnapshot fetches and replaces exactly one complete snapshot when invoked as due.
export async function synchronizeSnapshot(
  signal: AbortSignal,
  fetchSnapshot: SnapshotFetch = globalThis.fetch,
  replaceSnapshot: SnapshotReplacement = replaceActiveSnapshot,
): Promise<SnapshotV1> {
  signal.throwIfAborted();

  let response: Response;
  try {
    response = await fetchSnapshot("/api/snapshot", {
      cache: "no-store",
      method: "GET",
      signal,
    });
  } catch (cause) {
    signal.throwIfAborted();
    throw new SnapshotSynchronizationError("network", cause);
  }
  signal.throwIfAborted();

  if (!response.ok) {
    throw new SnapshotSynchronizationError(
      response.status === 410 ? "gone" : "http",
      undefined,
      response.status,
    );
  }

  signal.throwIfAborted();
  let serialized: string;
  try {
    serialized = await response.text();
  } catch (cause) {
    signal.throwIfAborted();
    throw new SnapshotSynchronizationError("network", cause);
  }
  signal.throwIfAborted();
  if (serialized.length === 0) {
    throw new SnapshotSynchronizationError("invalid-json");
  }

  try {
    return await replaceSnapshot(serialized, signal);
  } catch (cause) {
    signal.throwIfAborted();
    if (cause instanceof SnapshotError) {
      throw new SnapshotSynchronizationError(cause.kind, cause);
    }
    if (cause instanceof SnapshotReplacementError) {
      throw new SnapshotSynchronizationError(cause.kind, cause);
    }
    throw new SnapshotSynchronizationError("storage", cause);
  }
}
