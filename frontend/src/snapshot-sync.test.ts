// This module proves one-request synchronization, classification, and exact cancellation propagation.

import { describe, expect, it, vi } from "vitest";

import { SnapshotError, type SnapshotV1 } from "./snapshot";
import { SnapshotReplacementError } from "./snapshot-storage";
import {
  SnapshotSynchronizationError,
  synchronizeSnapshot,
} from "./snapshot-sync";

describe("one due snapshot request", () => {
  it("reads one complete response and accepts backend authority without comparison", async () => {
    const signal = new AbortController().signal;
    const fetchSnapshot = vi.fn(async () => new Response(serialized));
    const replace = vi.fn(async () => olderSameIdSnapshot);

    const result = await synchronizeSnapshot(signal, fetchSnapshot, replace);

    expect(result).toBe(olderSameIdSnapshot);
    expect(fetchSnapshot).toHaveBeenCalledOnce();
    expect(fetchSnapshot).toHaveBeenCalledWith("/api/snapshot", {
      cache: "no-store",
      method: "GET",
      signal,
    });
    expect(replace).toHaveBeenCalledOnce();
    expect(replace).toHaveBeenCalledWith(serialized, signal);
  });

  it.each([
    [410, "gone"],
    [404, "http"],
    [500, "http"],
  ] as const)(
    "classifies HTTP %i without reading its body or retrying",
    async (status, kind) => {
      const text = vi.fn(async () => "private response body");
      const fetchSnapshot = vi.fn(
        async () => ({ ok: false, status, text }) as unknown as Response,
      );
      const replace = vi.fn();

      await expect(
        synchronizeSnapshot(
          new AbortController().signal,
          fetchSnapshot,
          replace,
        ),
      ).rejects.toMatchObject({ kind, status });

      expect(fetchSnapshot).toHaveBeenCalledOnce();
      expect(text).not.toHaveBeenCalled();
      expect(replace).not.toHaveBeenCalled();
    },
  );

  it.each([
    ["empty 200 response", () => new Response("")],
    [
      "bodyless successful 204 response",
      () => new Response(null, { status: 204 }),
    ],
  ] as const)("classifies %s before storage", async (_, response) => {
    const fetchSnapshot = vi.fn(async () => response());
    const replace = vi.fn();

    await expect(
      synchronizeSnapshot(new AbortController().signal, fetchSnapshot, replace),
    ).rejects.toMatchObject({ kind: "invalid-json" });

    expect(fetchSnapshot).toHaveBeenCalledOnce();
    expect(replace).not.toHaveBeenCalled();
  });

  it("distinguishes request and successful-body network failures", async () => {
    const requestCause = new TypeError("offline detail");
    await expect(
      synchronizeSnapshot(
        new AbortController().signal,
        vi.fn(async () => {
          throw requestCause;
        }),
        vi.fn(),
      ),
    ).rejects.toEqual(
      new SnapshotSynchronizationError("network", requestCause),
    );

    const bodyCause = new TypeError("body detail");
    const text = vi.fn(async () => {
      throw bodyCause;
    });
    await expect(
      synchronizeSnapshot(
        new AbortController().signal,
        vi.fn(
          async () => ({ ok: true, status: 200, text }) as unknown as Response,
        ),
        vi.fn(),
      ),
    ).rejects.toEqual(new SnapshotSynchronizationError("network", bodyCause));
    expect(text).toHaveBeenCalledOnce();
  });

  it.each([
    ["invalid-json", new SnapshotError("invalid-json", "snapshot", "bad")],
    [
      "invalid-contract",
      new SnapshotError("invalid-contract", "snapshot", "bad"),
    ],
    [
      "unsupported-version",
      new SnapshotError("unsupported-version", "snapshot", "bad"),
    ],
    ["quota", new SnapshotReplacementError("quota")],
    ["storage", new SnapshotReplacementError("storage")],
    ["activation", new SnapshotReplacementError("activation")],
  ] as const)("preserves classified %s causes", async (kind, cause) => {
    await expect(
      synchronizeSnapshot(
        new AbortController().signal,
        vi.fn(async () => new Response(serialized)),
        vi.fn(async () => {
          throw cause;
        }),
      ),
    ).rejects.toMatchObject({ kind, cause });
  });
});

describe("exact abort reason propagation", () => {
  it.each([
    new Error("object reason"),
    new DOMException("stop", "AbortError"),
    "primitive reason",
  ])("rejects a pre-aborted call with identical reason %#", async (reason) => {
    const controller = new AbortController();
    controller.abort(reason);
    const fetchSnapshot = vi.fn();

    try {
      await synchronizeSnapshot(controller.signal, fetchSnapshot, vi.fn());
      throw new Error("expected cancellation");
    } catch (cause) {
      expect(cause).toBe(reason);
    }
    expect(fetchSnapshot).not.toHaveBeenCalled();
  });

  it.each(["fetch", "body", "replacement"] as const)(
    "preserves the owner reason during %s",
    async (stage) => {
      const controller = new AbortController();
      const reason = { stage };
      const pending = deferred<Response>();
      const body = deferred<string>();
      const replacement = deferred<SnapshotV1>();
      const fetchSnapshot = vi.fn(async () =>
        stage === "fetch"
          ? pending.promise
          : ({
              ok: true,
              status: 200,
              text: () =>
                stage === "body" ? body.promise : Promise.resolve(serialized),
            } as unknown as Response),
      );
      const replace = vi.fn(() => replacement.promise);

      const synchronization = synchronizeSnapshot(
        controller.signal,
        fetchSnapshot,
        replace,
      );
      await Promise.resolve();
      if (stage === "replacement") {
        await Promise.resolve();
      }
      controller.abort(reason);
      if (stage === "fetch") {
        pending.reject(new DOMException("generated", "AbortError"));
      } else if (stage === "body") {
        body.reject(new DOMException("generated", "AbortError"));
      } else {
        replacement.reject(new DOMException("generated", "AbortError"));
      }

      try {
        await synchronization;
        throw new Error("expected cancellation");
      } catch (cause) {
        expect(cause).toBe(reason);
      }
      expect(fetchSnapshot).toHaveBeenCalledOnce();
    },
  );
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (cause: unknown) => void;
  const promise = new Promise<T>((complete, fail) => {
    resolve = complete;
    reject = fail;
  });
  return { promise, reject, resolve };
}

const serialized = JSON.stringify({
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
  observedAt: "2026-07-26T12:00:00Z",
  boards: { state: "failed" },
});

const olderSameIdSnapshot: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
  observedAt: "2026-07-25T12:00:00Z",
  boards: { state: "failed" },
};
