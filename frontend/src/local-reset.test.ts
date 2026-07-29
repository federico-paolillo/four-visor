// This module proves destructive local reset ordering, failure handling, and cache ownership.

import { IDBFactory } from "fake-indexeddb";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  LocalResetError,
  type LocalResetProgress,
  resetLocalData,
} from "./local-reset";
import { shellCachePrefix } from "./shell-cache";
import {
  jitterSeedKey,
  lineageMetadataStoreName,
  lineageRecordsStoreName,
  openSnapshotDatabase,
  settingsStoreName,
  snapshotDatabaseName,
} from "./snapshot-storage";

afterEach(() => {
  vi.unstubAllGlobals();
});

const rootScope = "https://four-visor.test/";

describe("confirmed local reset", () => {
  it("deletes the whole database and every owned cache before one reload", async () => {
    const factory = new IDBFactory();
    await seedAllDatabaseState(factory);
    const cache = memoryCacheStorage([
      `${shellCachePrefix}current`,
      `${shellCachePrefix}old`,
      "foreign-cache",
    ]);
    const reload = vi.fn(() => cache.events.push("reload"));
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    const progress: LocalResetProgress[] = [];
    const serviceWorkers = serviceWorkerDouble(rootScope, cache.events);

    await resetLocalData(
      factory,
      cache.storage,
      serviceWorkers.container,
      rootScope,
      reload,
      (value) => progress.push(value),
    );

    expect(await factory.databases()).not.toContainEqual(
      expect.objectContaining({ name: snapshotDatabaseName }),
    );
    expect(cache.names()).toEqual(["foreign-cache"]);
    expect(cache.events).toEqual([
      "keys",
      `delete:${shellCachePrefix}current`,
      `delete:${shellCachePrefix}old`,
      "keys",
      "registration:get",
      "registration:unregister",
      "registration:get",
      "keys",
      "keys",
      "registration:get",
      "reload",
    ]);
    expect(progress).toEqual([
      "deleting-database",
      "deleting-caches",
      "unregistering-service-worker",
    ]);
    expect(serviceWorkers.unregister).toHaveBeenCalledOnce();
    expect(reload).toHaveBeenCalledOnce();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("waits through a blocked deletion without touching caches or reload", async () => {
    const factory = new IDBFactory();
    const heldDatabase = await rawOpen(factory);
    const cache = memoryCacheStorage([`${shellCachePrefix}current`]);
    const reload = vi.fn();
    const progress: LocalResetProgress[] = [];

    const reset = resetLocalData(
      factory,
      cache.storage,
      undefined,
      rootScope,
      reload,
      (value) => progress.push(value),
    );
    await vi.waitFor(() => expect(progress).toContain("database-blocked"));
    expect(cache.events).toEqual([]);
    expect(reload).not.toHaveBeenCalled();

    heldDatabase.close();
    await reset;

    expect(progress).toEqual([
      "deleting-database",
      "database-blocked",
      "deleting-caches",
      "unregistering-service-worker",
    ]);
    expect(cache.names()).toEqual([]);
    expect(reload).toHaveBeenCalledOnce();
  });

  it("stops before caches and reload when database deletion fails", async () => {
    const cause = new DOMException("private detail", "UnknownError");
    const factory = failingDeleteFactory(cause);
    const cache = memoryCacheStorage([`${shellCachePrefix}current`]);
    const reload = vi.fn();

    await expect(
      resetLocalData(factory, cache.storage, undefined, rootScope, reload),
    ).rejects.toMatchObject({
      name: "LocalResetError",
      stage: "database",
      cause,
    });
    expect(cache.events).toEqual([]);
    expect(reload).not.toHaveBeenCalled();
  });

  it("treats false deletion as success only when final verification is clean", async () => {
    const absent = memoryCacheStorage([`${shellCachePrefix}current`]);
    absent.deleteResult = "false-and-remove";
    const reload = vi.fn();

    await resetLocalData(
      new IDBFactory(),
      absent.storage,
      undefined,
      rootScope,
      reload,
    );

    expect(absent.names()).toEqual([]);
    expect(reload).toHaveBeenCalledOnce();

    const remains = memoryCacheStorage([`${shellCachePrefix}current`]);
    remains.deleteResult = "false-and-keep";
    await expect(
      resetLocalData(
        new IDBFactory(),
        remains.storage,
        undefined,
        rootScope,
        vi.fn(),
      ),
    ).rejects.toMatchObject({ stage: "cache" });
    expect(remains.names()).toEqual([`${shellCachePrefix}current`]);
  });

  it("reports partial cache deletion and retries only the remainder", async () => {
    const factory = new IDBFactory();
    await seedAllDatabaseState(factory);
    const first = `${shellCachePrefix}first`;
    const second = `${shellCachePrefix}second`;
    const cache = memoryCacheStorage([first, second, "foreign-cache"]);
    cache.rejectDeletion = second;
    const reload = vi.fn();

    await expect(
      resetLocalData(factory, cache.storage, undefined, rootScope, reload),
    ).rejects.toMatchObject({ stage: "cache" });
    expect(cache.names()).toEqual([second, "foreign-cache"]);
    expect(reload).not.toHaveBeenCalled();
    expect(await factory.databases()).not.toContainEqual(
      expect.objectContaining({ name: snapshotDatabaseName }),
    );

    cache.rejectDeletion = undefined;
    await resetLocalData(factory, cache.storage, undefined, rootScope, reload);

    expect(cache.names()).toEqual(["foreign-cache"]);
    expect(reload).toHaveBeenCalledOnce();
  });

  it("preserves reload causes after local deletion has completed", async () => {
    const cause = new Error("private reload detail");
    await expect(
      resetLocalData(
        new IDBFactory(),
        memoryCacheStorage([]).storage,
        undefined,
        rootScope,
        () => {
          throw cause;
        },
      ),
    ).rejects.toEqual(new LocalResetError("reload", cause));
  });

  it("stops before reload when root registration cleanup fails", async () => {
    const cause = new Error("private unregister detail");
    const serviceWorkers = serviceWorkerDouble();
    serviceWorkers.unregister.mockRejectedValueOnce(cause);
    const reload = vi.fn();

    await expect(
      resetLocalData(
        new IDBFactory(),
        memoryCacheStorage([]).storage,
        serviceWorkers.container,
        rootScope,
        reload,
      ),
    ).rejects.toEqual(new LocalResetError("service-worker", cause));
    expect(reload).not.toHaveBeenCalled();

    await resetLocalData(
      new IDBFactory(),
      memoryCacheStorage([]).storage,
      serviceWorkers.container,
      rootScope,
      reload,
    );
    expect(reload).toHaveBeenCalledOnce();
  });

  it("waits for the app registration before touching caches or registrations", async () => {
    const appRegistration = deferred<void>();
    const factory = new IDBFactory();
    await seedAllDatabaseState(factory);
    const cache = memoryCacheStorage([`${shellCachePrefix}current`]);
    const serviceWorkers = serviceWorkerDouble();
    const reload = vi.fn();

    const reset = resetLocalData(
      factory,
      cache.storage,
      serviceWorkers.container,
      rootScope,
      reload,
      undefined,
      appRegistration.promise,
    );
    await vi.waitFor(async () =>
      expect(await factory.databases()).not.toContainEqual(
        expect.objectContaining({ name: snapshotDatabaseName }),
      ),
    );
    await Promise.resolve();

    expect(cache.events).toEqual([]);
    expect(serviceWorkers.container.getRegistration).not.toHaveBeenCalled();
    expect(reload).not.toHaveBeenCalled();

    appRegistration.resolve();
    await reset;
    expect(reload).toHaveBeenCalledOnce();
  });

  it("continues cleanup after the app registration rejects", async () => {
    const reload = vi.fn();
    const appRegistration = Promise.reject(
      new Error("private registration detail"),
    );
    void appRegistration.catch(() => {});

    await resetLocalData(
      new IDBFactory(),
      memoryCacheStorage([]).storage,
      undefined,
      rootScope,
      reload,
      undefined,
      appRegistration,
    );

    expect(reload).toHaveBeenCalledOnce();
  });

  it("fails when an exact root registration appears after the first lookup", async () => {
    const registration = {
      scope: rootScope,
      unregister: vi.fn(async () => true),
    };
    const serviceWorkers = {
      getRegistration: vi
        .fn<() => Promise<typeof registration | undefined>>()
        .mockResolvedValueOnce(undefined)
        .mockResolvedValueOnce(registration),
    };
    const reload = vi.fn();

    await expect(
      resetLocalData(
        new IDBFactory(),
        memoryCacheStorage([]).storage,
        serviceWorkers,
        rootScope,
        reload,
      ),
    ).rejects.toMatchObject({ stage: "service-worker" });
    expect(registration.unregister).not.toHaveBeenCalled();
    expect(serviceWorkers.getRegistration).toHaveBeenCalledTimes(2);
    expect(reload).not.toHaveBeenCalled();
  });

  it("checks through the final postcondition when the root registration is missing", async () => {
    const serviceWorkers = {
      getRegistration: vi.fn(async () => undefined),
    };
    const reload = vi.fn();

    await resetLocalData(
      new IDBFactory(),
      memoryCacheStorage([]).storage,
      serviceWorkers,
      rootScope,
      reload,
    );

    expect(serviceWorkers.getRegistration).toHaveBeenCalledTimes(3);
    expect(reload).toHaveBeenCalledOnce();
  });

  it("removes an owned cache recreated after initial verification", async () => {
    const recreated = `${shellCachePrefix}recreated`;
    const cache = memoryCacheStorage([]);
    cache.injectCacheOnKeysCall = { call: 3, name: recreated };
    const reload = vi.fn();

    await resetLocalData(
      new IDBFactory(),
      cache.storage,
      undefined,
      rootScope,
      reload,
    );

    expect(cache.events).toContain(`delete:${recreated}`);
    expect(cache.names()).toEqual([]);
    expect(reload).toHaveBeenCalledOnce();
  });

  it("rejects a root registration recreated during final cache cleanup", async () => {
    const cache = memoryCacheStorage([]);
    let registration:
      | Pick<ServiceWorkerRegistration, "scope" | "unregister">
      | undefined;
    const unregister = vi.fn(async () => {
      registration = undefined;
      return true;
    });
    const rootRegistration = { scope: rootScope, unregister };
    registration = rootRegistration;
    const serviceWorkers = {
      getRegistration: vi.fn(async () => registration),
    };
    let keysCalls = 0;
    const cacheStorage = {
      delete: cache.storage.delete,
      async keys() {
        keysCalls += 1;
        if (keysCalls === 3) {
          registration = rootRegistration;
        }
        return cache.storage.keys();
      },
    };
    const reload = vi.fn();

    await expect(
      resetLocalData(
        new IDBFactory(),
        cacheStorage,
        serviceWorkers,
        rootScope,
        reload,
      ),
    ).rejects.toMatchObject({
      cause: expect.any(Error),
      stage: "service-worker",
    });
    expect(serviceWorkers.getRegistration).toHaveBeenCalledTimes(3);
    expect(unregister).toHaveBeenCalledOnce();
    expect(reload).not.toHaveBeenCalled();
  });

  it("does not reload when an owned cache appears during final verification", async () => {
    const remaining = `${shellCachePrefix}remaining`;
    const cache = memoryCacheStorage([]);
    cache.injectCacheOnKeysCall = { call: 4, name: remaining };
    const reload = vi.fn();

    await expect(
      resetLocalData(
        new IDBFactory(),
        cache.storage,
        undefined,
        rootScope,
        reload,
      ),
    ).rejects.toMatchObject({ stage: "cache" });
    expect(cache.names()).toEqual([remaining]);
    expect(reload).not.toHaveBeenCalled();
  });

  it("rejects an unregister race when the root registration remains", async () => {
    const reload = vi.fn();
    const registration = {
      scope: rootScope,
      unregister: vi.fn(async () => false),
    };
    const serviceWorkers = {
      getRegistration: vi.fn(async () => registration),
    };

    await expect(
      resetLocalData(
        new IDBFactory(),
        memoryCacheStorage([]).storage,
        serviceWorkers,
        rootScope,
        reload,
      ),
    ).rejects.toMatchObject({
      cause: expect.any(Error),
      stage: "service-worker",
    });
    expect(registration.unregister).toHaveBeenCalledOnce();
    expect(reload).not.toHaveBeenCalled();
  });

  it("accepts a false unregister result only when the root is absent", async () => {
    let registration:
      | Pick<ServiceWorkerRegistration, "scope" | "unregister">
      | undefined;
    const unregister = vi.fn(async () => {
      registration = undefined;
      return false;
    });
    registration = { scope: rootScope, unregister };
    const serviceWorkers = {
      getRegistration: vi.fn(async () => registration),
    };
    const reload = vi.fn();

    await resetLocalData(
      new IDBFactory(),
      memoryCacheStorage([]).storage,
      serviceWorkers,
      rootScope,
      reload,
    );

    expect(unregister).toHaveBeenCalledOnce();
    expect(serviceWorkers.getRegistration).toHaveBeenCalledTimes(3);
    expect(reload).toHaveBeenCalledOnce();
  });

  it("leaves a non-root registration untouched", async () => {
    const serviceWorkers = serviceWorkerDouble(
      "https://four-visor.test/other/",
    );
    const reload = vi.fn();

    await resetLocalData(
      new IDBFactory(),
      memoryCacheStorage([]).storage,
      serviceWorkers.container,
      rootScope,
      reload,
    );

    expect(serviceWorkers.unregister).not.toHaveBeenCalled();
    expect(reload).toHaveBeenCalledOnce();
  });
});

async function seedAllDatabaseState(factory: IDBFactory): Promise<void> {
  const database = await openSnapshotDatabase(factory);
  const transaction = database.transaction(
    [lineageMetadataStoreName, lineageRecordsStoreName, settingsStoreName],
    "readwrite",
  );
  const done = transactionDone(transaction);
  transaction.objectStore(lineageMetadataStoreName).put({
    slot: "active",
    storageKey: "11111111-1111-4111-8111-111111111111",
    recordCount: 1,
    byteLength: 1,
    sha256: "0".repeat(64),
  });
  transaction.objectStore(lineageMetadataStoreName).put({
    slot: "incoming",
    storageKey: "22222222-2222-4222-8222-222222222222",
    recordCount: 1,
    byteLength: 1,
    sha256: "0".repeat(64),
  });
  transaction.objectStore(lineageRecordsStoreName).put({
    storageKey: "11111111-1111-4111-8111-111111111111",
    index: 0,
    bytes: new Uint8Array([1]),
  });
  transaction.objectStore(lineageRecordsStoreName).put({
    storageKey: "22222222-2222-4222-8222-222222222222",
    index: 0,
    bytes: new Uint8Array([2]),
  });
  transaction
    .objectStore(settingsStoreName)
    .put(new Uint8Array([3]), jitterSeedKey);
  await done;
  database.close();
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}

function rawOpen(factory: IDBFactory): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = factory.open(snapshotDatabaseName, 1);
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

function failingDeleteFactory(cause: DOMException): IDBFactory {
  return {
    deleteDatabase() {
      const request = { error: cause } as unknown as IDBOpenDBRequest;
      queueMicrotask(() => request.onerror?.call(request, new Event("error")));
      return request;
    },
  } as unknown as IDBFactory;
}

function memoryCacheStorage(initial: readonly string[]) {
  const names = new Set(initial);
  const events: string[] = [];
  let keysCalls = 0;
  let deleteResult: "true" | "false-and-remove" | "false-and-keep" = "true";
  let injectCacheOnKeysCall:
    | { readonly call: number; readonly name: string }
    | undefined;
  let rejectDeletion: string | undefined;

  return {
    events,
    storage: {
      async keys() {
        events.push("keys");
        keysCalls += 1;
        if (injectCacheOnKeysCall?.call === keysCalls) {
          names.add(injectCacheOnKeysCall.name);
        }
        return [...names];
      },
      async delete(cacheName: string) {
        events.push(`delete:${cacheName}`);
        if (cacheName === rejectDeletion) {
          throw new Error("simulated cache deletion failure");
        }
        if (deleteResult !== "false-and-keep") {
          names.delete(cacheName);
        }
        return deleteResult === "true";
      },
    },
    names: () => [...names],
    get deleteResult() {
      return deleteResult;
    },
    set deleteResult(value) {
      deleteResult = value;
    },
    get rejectDeletion() {
      return rejectDeletion;
    },
    set rejectDeletion(value) {
      rejectDeletion = value;
    },
    get injectCacheOnKeysCall() {
      return injectCacheOnKeysCall;
    },
    set injectCacheOnKeysCall(value) {
      injectCacheOnKeysCall = value;
    },
  };
}

function serviceWorkerDouble(scope = rootScope, events?: string[]) {
  let registration:
    | Pick<ServiceWorkerRegistration, "scope" | "unregister">
    | undefined;
  const unregister = vi.fn(async () => {
    events?.push("registration:unregister");
    registration = undefined;
    return true;
  });
  registration = { scope, unregister };

  return {
    container: {
      getRegistration: vi.fn(async () => {
        events?.push("registration:get");
        return registration;
      }),
    },
    unregister,
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}
