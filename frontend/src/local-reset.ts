// This module deletes every Four Visor-owned browser store before reloading.

import { ownedShellCacheNames } from "./shell-cache?application";
import { snapshotDatabaseName } from "./snapshot-storage";

export const resetConfirmationText =
  "Reset local 4Visor data on this device? This local-only action cannot be undone. It deletes the cached snapshot and incoming data, loses cached snapshot continuity and the stable local jitter, and removes all 4Visor application-shell caches. It does not reset server data.";

export type LocalResetStage =
  | "database"
  | "cache"
  | "service-worker"
  | "reload";
export type LocalResetProgress =
  | "deleting-database"
  | "database-blocked"
  | "deleting-caches"
  | "unregistering-service-worker";

// LocalResetError preserves the failed local stage without exposing browser diagnostics.
export class LocalResetError extends Error {
  readonly cause: unknown;
  readonly stage: LocalResetStage;

  constructor(stage: LocalResetStage, cause?: unknown) {
    super("local data could not be fully reset");
    this.name = "LocalResetError";
    this.stage = stage;
    this.cause = cause;
  }
}

type ResetCacheStorage = Pick<CacheStorage, "delete" | "keys">;
type ResetServiceWorkers = {
  getRegistration(
    clientURL?: string,
  ): Promise<
    Pick<ServiceWorkerRegistration, "scope" | "unregister"> | undefined
  >;
};

// resetLocalData removes owned storage and registration state before one reload.
export async function resetLocalData(
  factory: IDBFactory | undefined,
  cacheStorage: ResetCacheStorage,
  serviceWorkers: ResetServiceWorkers | undefined,
  rootScope: string,
  reload: () => void,
  reportProgress: (progress: LocalResetProgress) => void = () => {},
  appRegistration: Promise<unknown> = Promise.resolve(),
): Promise<void> {
  reportProgress("deleting-database");
  await deleteSnapshotDatabase(factory, () =>
    reportProgress("database-blocked"),
  );

  try {
    await appRegistration;
  } catch {
    // Startup registration failure is non-blocking; postconditions decide reset success.
  }

  reportProgress("deleting-caches");
  await deleteOwnedCaches(cacheStorage);

  reportProgress("unregistering-service-worker");
  await unregisterRootServiceWorker(serviceWorkers, rootScope);
  await assertNoRootServiceWorker(serviceWorkers, rootScope);
  // Unregistration can race a worker recreating its cache, so clean owned caches again.
  await deleteOwnedCaches(cacheStorage);
  await assertNoRootServiceWorker(serviceWorkers, rootScope);

  try {
    reload();
  } catch (cause) {
    throw new LocalResetError("reload", cause);
  }
}

async function deleteOwnedCaches(
  cacheStorage: ResetCacheStorage,
): Promise<void> {
  try {
    for (const cacheName of ownedShellCacheNames(await cacheStorage.keys())) {
      await cacheStorage.delete(cacheName);
    }
    if (ownedShellCacheNames(await cacheStorage.keys()).length !== 0) {
      throw new Error("owned shell cache remains");
    }
  } catch (cause) {
    throw new LocalResetError("cache", cause);
  }
}

async function unregisterRootServiceWorker(
  serviceWorkers: ResetServiceWorkers | undefined,
  rootScope: string,
): Promise<void> {
  if (serviceWorkers === undefined) {
    return;
  }

  try {
    const registration = await serviceWorkers.getRegistration(rootScope);
    if (registration?.scope === rootScope) {
      await registration.unregister();
    }
  } catch (cause) {
    throw new LocalResetError("service-worker", cause);
  }
}

async function assertNoRootServiceWorker(
  serviceWorkers: ResetServiceWorkers | undefined,
  rootScope: string,
): Promise<void> {
  if (serviceWorkers === undefined) {
    return;
  }

  try {
    if (
      (await serviceWorkers.getRegistration(rootScope))?.scope === rootScope
    ) {
      throw new Error("root service worker registration remains");
    }
  } catch (cause) {
    throw new LocalResetError("service-worker", cause);
  }
}

function deleteSnapshotDatabase(
  factory: IDBFactory | undefined,
  blocked: () => void,
): Promise<void> {
  if (factory === undefined) {
    return Promise.reject(new LocalResetError("database"));
  }

  let request: IDBOpenDBRequest;
  try {
    request = factory.deleteDatabase(snapshotDatabaseName);
  } catch (cause) {
    return Promise.reject(new LocalResetError("database", cause));
  }

  return new Promise((resolve, reject) => {
    request.onblocked = blocked;
    request.onerror = () =>
      reject(new LocalResetError("database", request.error));
    request.onsuccess = () => resolve();
  });
}
