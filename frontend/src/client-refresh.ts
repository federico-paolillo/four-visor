// This module owns the private installation jitter and fixed browser refresh cadence.

import {
  jitterSeedKey,
  openSnapshotDatabase,
  SnapshotStorageError,
  settingsStoreName,
} from "./snapshot-storage";

export const clientRefreshInterval = 60 * 60 * 1_000;
const minimumClientRefreshJitter = 5_000;
const maximumClientRefreshJitter = 60_000;

const jitterOutcomeCount =
  (maximumClientRefreshJitter - minimumClientRefreshJitter) / 1_000 + 1;
const unbiasedSeedLimit =
  Math.floor(256 / jitterOutcomeCount) * jitterOutcomeCount;
const refreshLockName = "four-visor-snapshot-refresh";

export type DueSynchronization = (signal: AbortSignal) => Promise<void>;

// deriveClientRefreshJitter maps one persisted unbiased seed to an integer-second offset.
export function deriveClientRefreshJitter(seed: unknown): number {
  if (
    !(seed instanceof Uint8Array) ||
    seed.byteLength !== 1 ||
    seed[0] === undefined ||
    seed[0] >= unbiasedSeedLimit
  ) {
    throw new SnapshotStorageError("corrupt");
  }

  return minimumClientRefreshJitter + (seed[0] % jitterOutcomeCount) * 1_000;
}

// loadOrCreateClientRefreshJitter atomically initializes and reuses the private IndexedDB seed.
export async function loadOrCreateClientRefreshJitter(
  factory: IDBFactory | undefined = globalThis.indexedDB,
  cryptoApi: Pick<Crypto, "getRandomValues"> = globalThis.crypto,
): Promise<number> {
  let database: IDBDatabase | undefined;
  try {
    database = await openSnapshotDatabase(factory);
    const transaction = database.transaction(settingsStoreName, "readwrite");
    const completion = transactionDone(transaction);
    const request = transaction
      .objectStore(settingsStoreName)
      .get(jitterSeedKey);
    let seed: unknown;
    let operationFailure: unknown;

    request.onsuccess = () => {
      try {
        seed =
          request.result === undefined
            ? createJitterSeed(cryptoApi)
            : request.result;
        deriveClientRefreshJitter(seed);
        if (request.result === undefined) {
          transaction.objectStore(settingsStoreName).put(seed, jitterSeedKey);
        }
      } catch (cause) {
        operationFailure = cause;
        transaction.abort();
      }
    };

    try {
      await completion;
    } catch (cause) {
      throw operationFailure ?? cause;
    }
    return deriveClientRefreshJitter(seed);
  } catch (cause) {
    if (cause instanceof SnapshotStorageError) {
      throw cause;
    }
    throw new SnapshotStorageError("unavailable", cause);
  } finally {
    database?.close();
  }
}

// runClientRefresh waits for the stable offset and then consumes strict future hourly cadences.
export async function runClientRefresh(
  jitter: number,
  synchronizeWhenDue: DueSynchronization,
  signal: AbortSignal,
  locks: LockManager = globalThis.navigator.locks,
): Promise<void> {
  let nextDue = performance.now() + jitter;

  while (await waitUntil(nextDue, signal)) {
    if (signal.aborted) {
      return;
    }
    try {
      await locks.request(
        refreshLockName,
        { ifAvailable: true, mode: "exclusive" },
        async (lock) => {
          if (lock !== null && !signal.aborted) {
            await synchronizeWhenDue(signal);
          }
        },
      );
    } catch (cause) {
      if (signal.aborted) {
        return;
      }
      throw cause;
    }

    if (signal.aborted) {
      return;
    }
    const completedAt = performance.now();
    do {
      nextDue += clientRefreshInterval;
    } while (nextDue <= completedAt);
  }
}

function createJitterSeed(
  cryptoApi: Pick<Crypto, "getRandomValues">,
): Uint8Array {
  const seed = new Uint8Array(1);
  do {
    cryptoApi.getRandomValues(seed);
  } while ((seed[0] ?? unbiasedSeedLimit) >= unbiasedSeedLimit);
  return seed;
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}

function waitUntil(due: number, signal: AbortSignal): Promise<boolean> {
  if (signal.aborted) {
    return Promise.resolve(false);
  }

  return new Promise((resolve) => {
    const finish = (elapsed: boolean) => {
      clearTimeout(timeout);
      signal.removeEventListener("abort", onAbort);
      resolve(elapsed);
    };
    const onAbort = () => finish(false);
    const timeout = setTimeout(
      () => finish(true),
      Math.max(0, due - performance.now()),
    );
    signal.addEventListener("abort", onAbort, { once: true });
  });
}
