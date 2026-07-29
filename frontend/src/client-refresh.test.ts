// This module proves private jitter persistence and the fixed, non-overlapping refresh cadence.

import { IDBFactory } from "fake-indexeddb";
import { afterEach, describe, expect, it, vi } from "vitest";

import { type ApplicationState, startApplication } from "./app";
import {
  clientRefreshInterval,
  deriveClientRefreshJitter,
  loadOrCreateClientRefreshJitter,
  runClientRefresh,
} from "./client-refresh";
import { resetLocalData } from "./local-reset";
import type { SnapshotV1 } from "./snapshot";
import {
  jitterSeedKey,
  openSnapshotDatabase,
  settingsStoreName,
} from "./snapshot-storage";
import {
  SnapshotSynchronizationError,
  type SnapshotSynchronizationErrorKind,
  synchronizeSnapshot,
} from "./snapshot-sync";

afterEach(() => {
  vi.useRealTimers();
});

describe("installation-local jitter", () => {
  it("derives the inclusive integer-second range deterministically", () => {
    expect(deriveClientRefreshJitter(new Uint8Array([0]))).toBe(5_000);
    expect(deriveClientRefreshJitter(new Uint8Array([55]))).toBe(60_000);
    expect(deriveClientRefreshJitter(new Uint8Array([223]))).toBe(60_000);

    const outcomes = new Map<number, number>();
    for (let seed = 0; seed < 224; seed += 1) {
      const jitter = deriveClientRefreshJitter(new Uint8Array([seed]));
      expect(jitter).toBeGreaterThanOrEqual(5_000);
      expect(jitter).toBeLessThanOrEqual(60_000);
      outcomes.set(jitter, (outcomes.get(jitter) ?? 0) + 1);
    }
    expect([...outcomes.values()]).toEqual(Array(56).fill(4));
  });

  it.each([new Uint8Array(), new Uint8Array([224]), new Uint8Array([0, 1]), 3])(
    "rejects malformed persisted seed %#",
    (seed) => {
      expect(() => deriveClientRefreshJitter(seed)).toThrow(
        expect.objectContaining({ kind: "corrupt" }),
      );
    },
  );

  it("rejection-samples once, persists the seed, and reuses it on reload", async () => {
    const factory = new IDBFactory();
    const firstRandom = deterministicCrypto(255, 55);

    expect(
      await loadOrCreateClientRefreshJitter(factory, firstRandom.api),
    ).toBe(60_000);
    expect(firstRandom.getRandomValues).toHaveBeenCalledTimes(2);
    expect(await storedSeed(factory)).toEqual(new Uint8Array([55]));

    const reloadRandom = deterministicCrypto(0);
    expect(
      await loadOrCreateClientRefreshJitter(factory, reloadRandom.api),
    ).toBe(60_000);
    expect(reloadRandom.getRandomValues).not.toHaveBeenCalled();
  });

  it("serializes concurrent first activation into one stored seed", async () => {
    const factory = new IDBFactory();
    const random = deterministicCrypto(7, 31);

    const jitters = await Promise.all([
      loadOrCreateClientRefreshJitter(factory, random.api),
      loadOrCreateClientRefreshJitter(factory, random.api),
    ]);

    expect(jitters).toEqual([12_000, 12_000]);
    expect(random.getRandomValues).toHaveBeenCalledOnce();
    expect(await storedSeed(factory)).toEqual(new Uint8Array([7]));
  });

  it("classifies corrupt persisted seed without replacing it", async () => {
    const factory = new IDBFactory();
    await putSeed(factory, new Uint8Array([224]));
    const random = deterministicCrypto(0);

    await expect(
      loadOrCreateClientRefreshJitter(factory, random.api),
    ).rejects.toEqual(expect.objectContaining({ kind: "corrupt" }));
    expect(random.getRandomValues).not.toHaveBeenCalled();
    expect(await storedSeed(factory)).toEqual(new Uint8Array([224]));
  });

  it("regenerates only after the existing local reset deletes the database", async () => {
    const factory = new IDBFactory();
    expect(
      await loadOrCreateClientRefreshJitter(
        factory,
        deterministicCrypto(0).api,
      ),
    ).toBe(5_000);

    const reload = vi.fn();
    await resetLocalData(
      factory,
      { delete: async () => true, keys: async () => [] },
      undefined,
      "https://four-visor.test/",
      reload,
    );

    expect(reload).toHaveBeenCalledOnce();
    expect(
      await loadOrCreateClientRefreshJitter(
        factory,
        deterministicCrypto(55).api,
      ),
    ).toBe(60_000);
  });
});

describe("fixed client refresh cadence", () => {
  it("starts at D0 and then uses exact hourly Dn without accumulating jitter", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const synchronize = vi.fn(async () => {});
    const run = runClientRefresh(
      5_000,
      synchronize,
      controller.signal,
      availableLocks(),
    );

    await vi.advanceTimersByTimeAsync(4_999);
    expect(synchronize).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(synchronize).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(clientRefreshInterval - 1);
    expect(synchronize).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(synchronize).toHaveBeenCalledTimes(2);

    controller.abort();
    await run;
  });

  it("skips every cadence crossed by an active synchronization", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const first = deferred<void>();
    const synchronize = vi
      .fn<() => Promise<void>>()
      .mockReturnValueOnce(first.promise)
      .mockResolvedValue(undefined);
    const run = runClientRefresh(
      5_000,
      synchronize,
      controller.signal,
      availableLocks(),
    );

    await vi.advanceTimersByTimeAsync(5_000);
    expect(synchronize).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(2 * clientRefreshInterval);
    expect(synchronize).toHaveBeenCalledOnce();

    first.resolve();
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(clientRefreshInterval - 1);
    expect(synchronize).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(1);
    expect(synchronize).toHaveBeenCalledTimes(2);

    controller.abort();
    await run;
  });

  it.each([
    "network",
    "gone",
    "http",
    "invalid-json",
    "invalid-contract",
    "unsupported-version",
    "quota",
    "storage",
    "activation",
  ] satisfies readonly SnapshotSynchronizationErrorKind[])(
    "waits for the next cadence after %s failure",
    async (kind) => {
      vi.useFakeTimers();
      const synchronize = vi
        .fn<() => Promise<SnapshotV1>>()
        .mockRejectedValueOnce(new SnapshotSynchronizationError(kind))
        .mockResolvedValue(replacement);
      const application = await startApplication({
        confirm: () => true,
        load: async () => current,
        render: () => {},
        reset: async () => {},
        synchronize,
      });
      const controller = new AbortController();
      const run = runClientRefresh(
        5_000,
        application.synchronizeWhenDue,
        controller.signal,
        availableLocks(),
      );

      await vi.advanceTimersByTimeAsync(5_000);
      expect(synchronize).toHaveBeenCalledOnce();
      await vi.advanceTimersByTimeAsync(clientRefreshInterval - 1);
      expect(synchronize).toHaveBeenCalledOnce();
      await vi.advanceTimersByTimeAsync(1);
      expect(synchronize).toHaveBeenCalledTimes(2);

      controller.abort();
      await run;
    },
  );

  it("uses an exclusive non-waiting origin lock and skips a held cadence", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const locks = availableLocks(false, true);
    const synchronize = vi.fn(async () => {});
    const run = runClientRefresh(5_000, synchronize, controller.signal, locks);

    await vi.advanceTimersByTimeAsync(5_000);
    expect(synchronize).not.toHaveBeenCalled();
    expect(locks.request).toHaveBeenLastCalledWith(
      "four-visor-snapshot-refresh",
      {
        ifAvailable: true,
        mode: "exclusive",
      },
      expect.any(Function),
    );
    await vi.advanceTimersByTimeAsync(clientRefreshInterval);
    expect(synchronize).toHaveBeenCalledOnce();

    controller.abort();
    await run;
  });

  it("clears a pending timer and propagates cancellation into active work", async () => {
    vi.useFakeTimers();
    const waitingController = new AbortController();
    const waitingSync = vi.fn(async () => {});
    const waitingRun = runClientRefresh(
      5_000,
      waitingSync,
      waitingController.signal,
      availableLocks(),
    );
    waitingController.abort(new Error("waiting"));
    await waitingRun;
    await vi.advanceTimersByTimeAsync(5_000);
    expect(waitingSync).not.toHaveBeenCalled();

    const activeController = new AbortController();
    const activeSync = vi.fn(
      (signal: AbortSignal) =>
        new Promise<void>((resolve) =>
          signal.addEventListener("abort", () => resolve(), { once: true }),
        ),
    );
    const activeRun = runClientRefresh(
      5_000,
      activeSync,
      activeController.signal,
      availableLocks(),
    );
    await vi.advanceTimersByTimeAsync(5_000);
    expect(activeSync).toHaveBeenCalledWith(activeController.signal);
    activeController.abort(new Error("active"));
    await activeRun;
    expect(activeSync).toHaveBeenCalledOnce();
  });

  it("runs the US-010 boundary without real network and waits after failure", async () => {
    vi.useFakeTimers();
    const renders: ApplicationState[] = [];
    const fetchSnapshot = vi
      .fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>()
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValueOnce(new Response(JSON.stringify(replacement)));
    const replace = vi.fn(async () => replacement);
    const application = await startApplication({
      confirm: () => true,
      load: async () => current,
      render: (state) => renders.push(state),
      reset: async () => {},
      synchronize: (signal) =>
        synchronizeSnapshot(signal, fetchSnapshot, replace),
    });
    const controller = new AbortController();
    const run = runClientRefresh(
      5_000,
      application.synchronizeWhenDue,
      controller.signal,
      availableLocks(),
    );

    expect(latest(renders)).toEqual({ kind: "ready", snapshot: current });
    await vi.advanceTimersByTimeAsync(5_000);
    expect(latest(renders)).toMatchObject({
      kind: "synchronization-error",
      snapshot: current,
      error: { kind: "network" },
    });
    expect(fetchSnapshot).toHaveBeenCalledOnce();

    await vi.advanceTimersByTimeAsync(clientRefreshInterval - 1);
    expect(fetchSnapshot).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchSnapshot).toHaveBeenCalledTimes(2);
    expect(fetchSnapshot).toHaveBeenLastCalledWith("/api/snapshot", {
      cache: "no-store",
      method: "GET",
      signal: controller.signal,
    });
    expect(latest(renders)).toEqual({ kind: "ready", snapshot: replacement });

    controller.abort();
    await run;
  });
});

function deterministicCrypto(...values: number[]) {
  let index = 0;
  const getRandomValues = vi.fn((array: Uint8Array) => {
    array[0] = values[index] ?? 0;
    index += 1;
    return array;
  });
  return {
    api: { getRandomValues } as unknown as Pick<Crypto, "getRandomValues">,
    getRandomValues,
  };
}

function availableLocks(...availability: boolean[]): LockManager & {
  readonly request: ReturnType<typeof vi.fn>;
} {
  let call = 0;
  const request = vi.fn(
    async (
      _name: string,
      _options: LockOptions,
      callback: (lock: Lock | null) => Promise<void>,
    ) => {
      if (_options.ifAvailable && _options.signal !== undefined) {
        throw new DOMException(
          "ifAvailable and signal cannot be combined",
          "NotSupportedError",
        );
      }
      const available = availability[call] ?? true;
      call += 1;
      return callback(
        available ? ({ mode: "exclusive", name: "refresh" } as Lock) : null,
      );
    },
  );
  return { request } as unknown as LockManager & {
    readonly request: ReturnType<typeof vi.fn>;
  };
}

async function storedSeed(factory: IDBFactory): Promise<unknown> {
  const database = await openSnapshotDatabase(factory);
  const transaction = database.transaction(settingsStoreName, "readonly");
  const done = transactionDone(transaction);
  const request = transaction.objectStore(settingsStoreName).get(jitterSeedKey);
  await done;
  database.close();
  return request.result;
}

async function putSeed(factory: IDBFactory, seed: Uint8Array): Promise<void> {
  const database = await openSnapshotDatabase(factory);
  const transaction = database.transaction(settingsStoreName, "readwrite");
  const done = transactionDone(transaction);
  transaction.objectStore(settingsStoreName).put(seed, jitterSeedKey);
  await done;
  database.close();
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}

function latest<T>(values: readonly T[]): T {
  const value = values[values.length - 1];
  if (value === undefined) {
    throw new Error("expected a recorded value");
  }
  return value;
}

const current: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
  observedAt: "2026-07-26T12:00:00Z",
  boards: { state: "failed" },
};

const replacement: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y8Y",
  observedAt: "2026-07-27T12:00:00Z",
  boards: { state: "failed" },
};
