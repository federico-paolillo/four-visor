// This module proves the IndexedDB schema, codec, ownership audit, and active startup boundary.

import {
  IDBDatabase as FakeIDBDatabase,
  IDBFactory,
  IDBKeyRange,
} from "fake-indexeddb";
import { describe, expect, it, vi } from "vitest";

import {
  createLocalStorageKey,
  type EncodedLineage,
  encodeLineageRecords,
  jitterSeedKey,
  type LineageDescriptor,
  type LineageRecord,
  type LineageSlot,
  lineageMetadataStoreName,
  lineageRecordSize,
  lineageRecordsStoreName,
  loadActiveSnapshot,
  openSnapshotDatabase,
  replaceActiveSnapshot,
  SnapshotReplacementError,
  settingsStoreName,
  snapshotDatabaseName,
} from "./snapshot-storage";

const activeKey = "11111111-1111-4111-8111-111111111111";
const incomingKey = "22222222-2222-4222-8222-222222222222";
const replacementKey = "33333333-3333-4333-8333-333333333333";
const originalTransaction = FakeIDBDatabase.prototype.transaction;

describe("snapshot database schema and codec", () => {
  it("creates exactly three stores with no secondary indexes", async () => {
    const database = await openSnapshotDatabase(new IDBFactory());
    expect([...database.objectStoreNames]).toEqual([
      lineageMetadataStoreName,
      lineageRecordsStoreName,
      settingsStoreName,
    ]);

    const transaction = database.transaction(
      [lineageMetadataStoreName, lineageRecordsStoreName, settingsStoreName],
      "readonly",
    );
    const done = transactionDone(transaction);
    expect(transaction.objectStore(lineageMetadataStoreName).keyPath).toBe(
      "slot",
    );
    expect(transaction.objectStore(lineageRecordsStoreName).keyPath).toEqual([
      "storageKey",
      "index",
    ]);
    for (const storeName of [
      lineageMetadataStoreName,
      lineageRecordsStoreName,
      settingsStoreName,
    ]) {
      const store = transaction.objectStore(storeName);
      expect(store.autoIncrement).toBe(false);
      expect(store.indexNames.length).toBe(0);
    }
    await done;
    database.close();
    expect(jitterSeedKey).toBe("jitter-seed");
  });

  it("uses local UUID ownership and exact fixed-size UTF-8 records", async () => {
    const storageKey = createLocalStorageKey(crypto);
    const serialized = "é".repeat(40_000);
    const encoded = await encodeLineageRecords(
      "active",
      storageKey,
      serialized,
      crypto,
    );

    expect(storageKey).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(encoded.descriptor).toMatchObject({
      slot: "active",
      storageKey,
      recordCount: 2,
      byteLength: 80_000,
      sha256: expect.stringMatching(/^[0-9a-f]{64}$/),
    });
    expect(encoded.records.map(({ bytes }) => bytes.byteLength)).toEqual([
      lineageRecordSize,
      80_000 - lineageRecordSize,
    ]);
    expect(encoded.records.map(({ index }) => index)).toEqual([0, 1]);
    expect(JSON.stringify(encoded.descriptor)).not.toContain("lineageId");

    const exact = await encodeLineageRecords(
      "incoming",
      incomingKey,
      "x".repeat(lineageRecordSize),
      crypto,
    );
    expect(exact.records).toHaveLength(1);
    expect(exact.records[0]?.bytes).toHaveLength(lineageRecordSize);
  });

  it.each([lineageMetadataStoreName, settingsStoreName])(
    "rejects a constructible auto-increment %s store",
    async (autoIncrementStore) => {
      const factory = new IDBFactory();
      const database = await rawSchema(factory, (upgrade) => {
        upgrade.createObjectStore(lineageMetadataStoreName, {
          autoIncrement: autoIncrementStore === lineageMetadataStoreName,
          keyPath: "slot",
        });
        upgrade.createObjectStore(lineageRecordsStoreName, {
          keyPath: ["storageKey", "index"],
        });
        upgrade.createObjectStore(settingsStoreName, {
          autoIncrement: autoIncrementStore === settingsStoreName,
        });
      });
      database.close();

      await expect(openSnapshotDatabase(factory)).rejects.toMatchObject({
        kind: "corrupt",
      });
    },
  );

  it("asserts the platform forbids auto-increment with the compound record key", async () => {
    const factory = new IDBFactory();
    const database = await rawSchema(factory, (upgrade) => {
      expect(() =>
        upgrade.createObjectStore(lineageRecordsStoreName, {
          autoIncrement: true,
          keyPath: ["storageKey", "index"],
        }),
      ).toThrow(expect.objectContaining({ name: "InvalidAccessError" }));
      upgrade.createObjectStore(lineageMetadataStoreName, { keyPath: "slot" });
      upgrade.createObjectStore(lineageRecordsStoreName, {
        keyPath: ["storageKey", "index"],
      });
      upgrade.createObjectStore(settingsStoreName);
    });
    database.close();
  });
});

describe("startup lineage loading", () => {
  it("loads a valid active lineage and closes its connection", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const close = vi.spyOn(FakeIDBDatabase.prototype, "close");

    const snapshot = await loadActiveSnapshot(factory, crypto);

    expect(snapshot?.lineageId).toBe("01J1YQ7Y0M4S6R8T2V3W5X7Y9Z");
    expect(close).toHaveBeenCalled();
    close.mockRestore();
  });

  it("returns empty for a fresh database and for structural incoming only", async () => {
    expect(await loadActiveSnapshot(new IDBFactory(), crypto)).toBeUndefined();

    const factory = new IDBFactory();
    await seedLineage(factory, "incoming", "not valid JSON", incomingKey);
    expect(await loadActiveSnapshot(factory, crypto)).toBeUndefined();
  });

  it("does not trust or render an invalid but structurally complete incoming candidate", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    await seedLineage(factory, "incoming", "not valid JSON", incomingKey);

    const snapshot = await loadActiveSnapshot(factory, crypto);

    expect(snapshot?.lineageId).toBe("01J1YQ7Y0M4S6R8T2V3W5X7Y9Z");
  });

  it("keeps same-upstream-ID generations distinct by local key", async () => {
    const factory = new IDBFactory();
    const serialized = minimalSnapshot();
    const active = await seedLineage(factory, "active", serialized, activeKey);
    const incoming = await seedLineage(
      factory,
      "incoming",
      serialized,
      incomingKey,
    );

    expect(active.descriptor.storageKey).not.toBe(
      incoming.descriptor.storageKey,
    );
    expect((await loadActiveSnapshot(factory, crypto))?.lineageId).toBe(
      "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
    );
  });

  it("round-trips a private maximum-cardinality fixture across many records", async () => {
    const factory = new IDBFactory();
    const encoded = await seedLineage(
      factory,
      "active",
      JSON.stringify(maximumCardinalitySnapshot()),
      activeKey,
    );

    expect(encoded.descriptor.recordCount).toBeGreaterThan(1);
    const snapshot = await loadActiveSnapshot(factory, crypto);
    if (snapshot?.boards.state !== "present") {
      throw new Error("maximum fixture boards must be present");
    }
    const catalog = snapshot.boards.items[0]?.catalog;
    if (catalog?.state !== "present") {
      throw new Error("maximum fixture catalog must be present");
    }
    const threads = catalog.pages[0]?.threads;
    expect(threads).toHaveLength(250);
    expect(threads?.map(({ summary }) => summary.no)).toEqual(
      Array.from({ length: 250 }, (_, index) => index),
    );
    for (const [threadIndex, entry] of threads?.entries() ?? []) {
      if (entry.thread?.state !== "present") {
        throw new Error("maximum fixture thread must be present");
      }
      expect(entry.thread.posts).toHaveLength(250);
      expect(entry.thread.posts[0]?.no).toBe(threadIndex * 1_000);
      expect(entry.thread.posts[249]?.no).toBe(threadIndex * 1_000 + 249);
    }
  });
});

describe("complete lineage replacement", () => {
  it("promotes a same-ID older snapshot under a fresh local key and removes the old generation", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);

    const snapshot = await replaceActiveSnapshot(
      olderSameIdSnapshot(),
      new AbortController().signal,
      factory,
      IDBKeyRange,
      fixedCrypto(incomingKey),
    );
    const state = await lineageState(factory);

    expect(snapshot.observedAt).toBe("2026-07-25T12:00:00Z");
    expect(state.metadata).toEqual([
      expect.objectContaining({ slot: "active", storageKey: incomingKey }),
    ]);
    expect(new Set(state.records.map(({ storageKey }) => storageKey))).toEqual(
      new Set([incomingKey]),
    );
    expect((await loadActiveSnapshot(factory, crypto))?.observedAt).toBe(
      "2026-07-25T12:00:00Z",
    );
  });

  it("activates the first complete snapshot without creating a second owner", async () => {
    const factory = new IDBFactory();

    await replaceActiveSnapshot(
      minimalSnapshot(),
      new AbortController().signal,
      factory,
      IDBKeyRange,
      fixedCrypto(incomingKey),
    );

    const state = await lineageState(factory);
    expect(state.metadata).toEqual([
      expect.objectContaining({ slot: "active", storageKey: incomingKey }),
    ]);
    expect(
      state.records.every(({ storageKey }) => storageKey === incomingKey),
    ).toBe(true);
  });

  it("replaces a previous inactive generation while staging the next one", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    await seedLineage(factory, "incoming", "obsolete incoming", incomingKey);

    await expect(
      replaceActiveSnapshot(
        "not valid JSON",
        new AbortController().signal,
        factory,
        IDBKeyRange,
        fixedCrypto(replacementKey),
      ),
    ).rejects.toMatchObject({ kind: "invalid-json" });

    const state = await lineageState(factory);
    expect(state.metadata).toEqual([
      expect.objectContaining({ slot: "active", storageKey: activeKey }),
      expect.objectContaining({
        slot: "incoming",
        storageKey: replacementKey,
      }),
    ]);
    expect(recordsFor(state, incomingKey)).toEqual([]);
    expect(recordsFor(state, replacementKey)).not.toEqual([]);
  });

  it("validates the staged bytes rather than the downloaded string", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const before = await lineageState(factory);
    let writes = 0;
    const transaction = vi
      .spyOn(FakeIDBDatabase.prototype, "transaction")
      .mockImplementation(function (this: IDBDatabase, ...args) {
        const result = Reflect.apply(
          originalTransaction,
          this,
          args,
        ) as IDBTransaction;
        if (args[1] === "readwrite" && ++writes === 1) {
          result.addEventListener(
            "complete",
            () => {
              const corruption = Reflect.apply(originalTransaction, this, [
                lineageRecordsStoreName,
                "readwrite",
              ]) as IDBTransaction;
              corruption.objectStore(lineageRecordsStoreName).put({
                storageKey: incomingKey,
                index: 0,
                bytes: new TextEncoder().encode("corrupt"),
              });
            },
            { once: true },
          );
        }
        return result;
      });

    await expect(
      replaceActiveSnapshot(
        minimalSnapshot(),
        new AbortController().signal,
        factory,
        IDBKeyRange,
        fixedCrypto(incomingKey),
      ),
    ).rejects.toMatchObject({ kind: "storage" });
    transaction.mockRestore();

    const after = await lineageState(factory);
    expect(recordsFor(after, activeKey)).toEqual(recordsFor(before, activeKey));
    expect(after.metadata).toContainEqual(
      expect.objectContaining({ slot: "incoming", storageKey: incomingKey }),
    );
  });

  it.each([
    ["invalid-json", "not valid JSON"],
    ["invalid-contract", JSON.stringify({})],
    [
      "unsupported-version",
      JSON.stringify({
        schemaVersion: 2,
        lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
        observedAt: "2026-07-26T12:00:00Z",
        boards: { state: "failed" },
      }),
    ],
  ] as const)(
    "keeps %s candidates staged and the old active bytes untouched",
    async (kind, serialized) => {
      const factory = new IDBFactory();
      await seedLineage(factory, "active", minimalSnapshot(), activeKey);
      const before = await lineageState(factory);

      await expect(
        replaceActiveSnapshot(
          serialized,
          new AbortController().signal,
          factory,
          IDBKeyRange,
          fixedCrypto(incomingKey),
        ),
      ).rejects.toMatchObject({ kind });

      const after = await lineageState(factory);
      expect(after.metadata).toEqual([
        expect.objectContaining({ slot: "active", storageKey: activeKey }),
        expect.objectContaining({ slot: "incoming", storageKey: incomingKey }),
      ]);
      expect(recordsFor(after, activeKey)).toEqual(
        recordsFor(before, activeKey),
      );
      expect((await loadActiveSnapshot(factory, crypto))?.lineageId).toBe(
        "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
      );
    },
  );

  it.each([
    ["quota", new DOMException("private", "QuotaExceededError")],
    ["storage", new DOMException("private", "UnknownError")],
  ] as const)(
    "classifies %s before staging without touching active",
    async (kind, failure) => {
      const factory = new IDBFactory();
      await seedLineage(factory, "active", minimalSnapshot(), activeKey);
      const before = await lineageState(factory);
      const transaction = vi
        .spyOn(FakeIDBDatabase.prototype, "transaction")
        .mockImplementationOnce(function (this: IDBDatabase, ...args) {
          return Reflect.apply(
            originalTransaction,
            this,
            args,
          ) as IDBTransaction;
        })
        .mockImplementationOnce(() => {
          throw failure;
        });

      await expect(
        replaceActiveSnapshot(
          minimalSnapshot(),
          new AbortController().signal,
          factory,
          IDBKeyRange,
          fixedCrypto(incomingKey),
        ),
      ).rejects.toMatchObject({ kind, cause: failure });

      transaction.mockRestore();
      expect(await lineageState(factory)).toEqual(before);
    },
  );

  it("rolls back an activation failure while retaining the validated incoming generation", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const before = await lineageState(factory);
    let writes = 0;
    const transaction = vi
      .spyOn(FakeIDBDatabase.prototype, "transaction")
      .mockImplementation(function (this: IDBDatabase, ...args) {
        const result = Reflect.apply(
          originalTransaction,
          this,
          args,
        ) as IDBTransaction;
        if (args[1] === "readwrite" && ++writes === 2) {
          queueMicrotask(() => result.abort());
        }
        return result;
      });

    await expect(
      replaceActiveSnapshot(
        olderSameIdSnapshot(),
        new AbortController().signal,
        factory,
        IDBKeyRange,
        fixedCrypto(incomingKey),
      ),
    ).rejects.toBeInstanceOf(SnapshotReplacementError);
    transaction.mockRestore();

    const after = await lineageState(factory);
    expect(after.metadata).toEqual([
      expect.objectContaining({ slot: "active", storageKey: activeKey }),
      expect.objectContaining({ slot: "incoming", storageKey: incomingKey }),
    ]);
    expect(recordsFor(after, activeKey)).toEqual(recordsFor(before, activeKey));
  });

  it("lets an active read captured before promotion finish from the old generation", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const digestStarted = deferred<void>();
    const continueDigest = deferred<void>();
    const delayedCrypto = {
      subtle: {
        async digest(algorithm: AlgorithmIdentifier, data: BufferSource) {
          digestStarted.resolve();
          await continueDigest.promise;
          return crypto.subtle.digest(algorithm, data);
        },
      } as SubtleCrypto,
    };

    const oldRead = loadActiveSnapshot(factory, delayedCrypto);
    await digestStarted.promise;
    await replaceActiveSnapshot(
      olderSameIdSnapshot(),
      new AbortController().signal,
      factory,
      IDBKeyRange,
      fixedCrypto(incomingKey),
    );
    continueDigest.resolve();

    expect((await oldRead)?.observedAt).toBe("2026-07-26T12:00:00Z");
    expect((await loadActiveSnapshot(factory, crypto))?.observedAt).toBe(
      "2026-07-25T12:00:00Z",
    );
  });
});

describe("replacement cancellation and connection ownership", () => {
  it("does not open when already aborted and preserves the exact reason", async () => {
    const reason = { stage: "before-open" };
    const controller = new AbortController();
    controller.abort(reason);
    const factory = { open: vi.fn() } as unknown as IDBFactory;

    await expectExactReason(
      replaceActiveSnapshot(
        minimalSnapshot(),
        controller.signal,
        factory,
        IDBKeyRange,
        fixedCrypto(incomingKey),
      ),
      reason,
    );
    expect(factory.open).not.toHaveBeenCalled();
  });

  it("closes a late successful open after pending cancellation", async () => {
    const close = vi.fn();
    const request = { result: { close } } as unknown as IDBOpenDBRequest;
    const factory = { open: vi.fn(() => request) } as unknown as IDBFactory;
    const controller = new AbortController();
    const reason = new Error("pending open canceled");
    const replacement = replaceActiveSnapshot(
      minimalSnapshot(),
      controller.signal,
      factory,
      IDBKeyRange,
      fixedCrypto(incomingKey),
    );

    controller.abort(reason);
    await expectExactReason(replacement, reason);
    request.onsuccess?.call(request, new Event("success"));

    expect(close).toHaveBeenCalledOnce();
  });

  it.each([1, 2])(
    "rolls back and preserves the exact reason during write transaction %i",
    async (targetWrite) => {
      const factory = new IDBFactory();
      await seedLineage(factory, "active", minimalSnapshot(), activeKey);
      const before = await lineageState(factory);
      const controller = new AbortController();
      const reason = { targetWrite };
      let writes = 0;
      const transaction = vi
        .spyOn(FakeIDBDatabase.prototype, "transaction")
        .mockImplementation(function (this: IDBDatabase, ...args) {
          const result = Reflect.apply(
            originalTransaction,
            this,
            args,
          ) as IDBTransaction;
          if (args[1] === "readwrite" && ++writes === targetWrite) {
            queueMicrotask(() => controller.abort(reason));
          }
          return result;
        });

      await expectExactReason(
        replaceActiveSnapshot(
          olderSameIdSnapshot(),
          controller.signal,
          factory,
          IDBKeyRange,
          fixedCrypto(incomingKey),
        ),
        reason,
      );
      transaction.mockRestore();

      const after = await lineageState(factory);
      expect(recordsFor(after, activeKey)).toEqual(
        recordsFor(before, activeKey),
      );
      expect(
        after.metadata.find(({ slot }) => slot === "active"),
      ).toMatchObject({
        storageKey: activeKey,
      });
      if (targetWrite === 1) {
        expect(after.metadata).toHaveLength(1);
      } else {
        expect(after.metadata).toContainEqual(
          expect.objectContaining({
            slot: "incoming",
            storageKey: incomingKey,
          }),
        );
      }
    },
  );

  it("preserves a committed incoming generation when canceled after stage", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const controller = new AbortController();
    const reason = { stage: "after-stage" };
    let writes = 0;
    const transaction = vi
      .spyOn(FakeIDBDatabase.prototype, "transaction")
      .mockImplementation(function (this: IDBDatabase, ...args) {
        const result = Reflect.apply(
          originalTransaction,
          this,
          args,
        ) as IDBTransaction;
        if (args[1] === "readwrite" && ++writes === 1) {
          result.addEventListener("complete", () => controller.abort(reason), {
            once: true,
          });
        }
        return result;
      });

    await expectExactReason(
      replaceActiveSnapshot(
        olderSameIdSnapshot(),
        controller.signal,
        factory,
        IDBKeyRange,
        fixedCrypto(incomingKey),
      ),
      reason,
    );
    transaction.mockRestore();

    expect((await lineageState(factory)).metadata).toEqual([
      expect.objectContaining({ slot: "active", storageKey: activeKey }),
      expect.objectContaining({ slot: "incoming", storageKey: incomingKey }),
    ]);
  });

  it("preserves the exact reason when canceled during staged digest validation", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const controller = new AbortController();
    const reason = { stage: "validation-digest" };
    let digests = 0;
    const cryptoApi = {
      randomUUID: () =>
        incomingKey as `${string}-${string}-${string}-${string}-${string}`,
      subtle: {
        async digest(algorithm: AlgorithmIdentifier, data: BufferSource) {
          const result = await crypto.subtle.digest(algorithm, data);
          if (++digests === 2) {
            controller.abort(reason);
          }
          return result;
        },
      } as SubtleCrypto,
    };

    await expectExactReason(
      replaceActiveSnapshot(
        olderSameIdSnapshot(),
        controller.signal,
        factory,
        IDBKeyRange,
        cryptoApi,
      ),
      reason,
    );

    expect((await lineageState(factory)).metadata).toContainEqual(
      expect.objectContaining({ slot: "incoming", storageKey: incomingKey }),
    );
  });

  it("does not start activation when canceled after validation capture", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const controller = new AbortController();
    const reason = { stage: "after-validation-capture" };
    let readonlyTransactions = 0;
    let writeTransactions = 0;
    const transaction = vi
      .spyOn(FakeIDBDatabase.prototype, "transaction")
      .mockImplementation(function (this: IDBDatabase, ...args) {
        const result = Reflect.apply(
          originalTransaction,
          this,
          args,
        ) as IDBTransaction;
        if (args[1] === "readonly" && ++readonlyTransactions === 2) {
          result.addEventListener("complete", () => controller.abort(reason), {
            once: true,
          });
        }
        if (args[1] === "readwrite") {
          writeTransactions += 1;
        }
        return result;
      });

    await expectExactReason(
      replaceActiveSnapshot(
        olderSameIdSnapshot(),
        controller.signal,
        factory,
        IDBKeyRange,
        fixedCrypto(incomingKey),
      ),
      reason,
    );
    transaction.mockRestore();

    expect(writeTransactions).toBe(1);
    expect((await lineageState(factory)).metadata).toContainEqual(
      expect.objectContaining({ slot: "incoming", storageKey: incomingKey }),
    );
  });

  it("lets activation commit win cancellation observed at completion", async () => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const controller = new AbortController();
    const reason = { stage: "activation-complete" };
    let writes = 0;
    const transaction = vi
      .spyOn(FakeIDBDatabase.prototype, "transaction")
      .mockImplementation(function (this: IDBDatabase, ...args) {
        const result = Reflect.apply(
          originalTransaction,
          this,
          args,
        ) as IDBTransaction;
        if (args[1] === "readwrite" && ++writes === 2) {
          result.addEventListener("complete", () => controller.abort(reason), {
            once: true,
          });
        }
        return result;
      });

    const snapshot = await replaceActiveSnapshot(
      olderSameIdSnapshot(),
      controller.signal,
      factory,
      IDBKeyRange,
      fixedCrypto(incomingKey),
    );
    transaction.mockRestore();

    expect(controller.signal.aborted).toBe(true);
    expect(snapshot.observedAt).toBe("2026-07-25T12:00:00Z");
    expect((await lineageState(factory)).metadata).toEqual([
      expect.objectContaining({ slot: "active", storageKey: incomingKey }),
    ]);
  });

  it("removes every signal listener and closes its transferred connection", async () => {
    const factory = new IDBFactory();
    const controller = new AbortController();
    const add = vi.spyOn(controller.signal, "addEventListener");
    const remove = vi.spyOn(controller.signal, "removeEventListener");
    const close = vi.spyOn(FakeIDBDatabase.prototype, "close");

    await replaceActiveSnapshot(
      minimalSnapshot(),
      controller.signal,
      factory,
      IDBKeyRange,
      fixedCrypto(incomingKey),
    );

    expect(remove.mock.calls.filter(([type]) => type === "abort")).toHaveLength(
      add.mock.calls.filter(([type]) => type === "abort").length,
    );
    expect(close).toHaveBeenCalledOnce();
    add.mockRestore();
    remove.mockRestore();
    close.mockRestore();
  });
});

describe("startup ownership and corruption audit", () => {
  it("rejects missing IndexedDB and preserves unavailable causes", async () => {
    await expect(loadActiveSnapshot(undefined, crypto)).rejects.toMatchObject({
      kind: "unavailable",
    });

    const cause = new DOMException("private detail", "SecurityError");
    const factory = {
      open() {
        throw cause;
      },
    } as unknown as IDBFactory;
    await expect(loadActiveSnapshot(factory, crypto)).rejects.toMatchObject({
      kind: "unavailable",
      cause,
    });
  });

  it("classifies a newer database as corrupt without migration", async () => {
    const factory = new IDBFactory();
    const database = await rawOpen(factory, 2);
    database.close();

    await expectCorrupt(factory);
  });

  it("closes a late connection after a blocked open", async () => {
    const close = vi.fn();
    const request = {
      result: { close },
    } as unknown as IDBOpenDBRequest;
    const factory = {
      open() {
        queueMicrotask(() => {
          request.onblocked?.call(
            request,
            new Event("blocked") as IDBVersionChangeEvent,
          );
          request.onsuccess?.call(request, new Event("success"));
        });
        return request;
      },
    } as unknown as IDBFactory;

    await expect(openSnapshotDatabase(factory)).rejects.toMatchObject({
      kind: "unavailable",
    });
    await Promise.resolve();
    expect(close).toHaveBeenCalledOnce();
  });

  it("installs a version-change close handler", async () => {
    const database = await openSnapshotDatabase(new IDBFactory());
    const close = vi.spyOn(database, "close");

    database.onversionchange?.(
      new Event("versionchange") as IDBVersionChangeEvent,
    );

    expect(close).toHaveBeenCalledOnce();
  });

  it.each([
    ["unknown metadata slot", corruptUnknownSlot],
    ["third descriptor", corruptThirdDescriptor],
    ["duplicate local ownership", corruptDuplicateOwnership],
    ["orphan third-generation record", corruptOrphanRecord],
    ["malformed compound record key", corruptMalformedRecordKey],
    ["missing record", corruptMissingRecord],
    ["extra record", corruptExtraRecord],
    ["malformed incoming metadata", corruptIncomingMetadata],
  ])("rejects %s", async (_, corrupt) => {
    const factory = new IDBFactory();
    await corrupt(factory);
    await expectCorrupt(factory);
  });

  it.each([
    ["active record value", corruptActiveRecordValue],
    ["active digest", corruptActiveDigest],
    ["active UTF-8", corruptActiveUtf8],
  ])("rejects corrupt %s", async (_, corrupt) => {
    const factory = new IDBFactory();
    await corrupt(factory);
    await expectCorrupt(factory);
  });

  it.each([
    [
      "extra field",
      (record: Record<string, unknown>) => ({ ...record, extra: true }),
    ],
    [
      "owner type",
      (record: Record<string, unknown>) => ({ ...record, storageKey: 3 }),
    ],
    [
      "owner mismatch",
      (record: Record<string, unknown>) => ({
        ...record,
        storageKey: "33333333-3333-4333-8333-333333333333",
      }),
    ],
    [
      "index type",
      (record: Record<string, unknown>) => ({ ...record, index: "0" }),
    ],
    [
      "bytes type",
      (record: Record<string, unknown>) => ({ ...record, bytes: [1] }),
    ],
    [
      "final byte length",
      (record: Record<string, unknown>) => ({
        ...record,
        bytes: new Uint8Array(lineageRecordSize),
      }),
    ],
  ] as const)("rejects corrupt incoming record %s", async (_, change) => {
    const factory = new IDBFactory();
    await seedLineage(factory, "active", minimalSnapshot(), activeKey);
    const incoming = await seedLineage(
      factory,
      "incoming",
      "opaque incoming",
      incomingKey,
    );
    await mutate(factory, lineageRecordsStoreName, (transaction) => {
      const records = transaction.objectStore(lineageRecordsStoreName);
      records.delete([incomingKey, 0]);
      records.put(
        change(incoming.records[0] as unknown as Record<string, unknown>),
      );
    });

    await expectCorrupt(factory);
  });

  it.each([-1, 1])(
    "rejects rebalanced incoming record boundaries with delta %i",
    async (delta) => {
      const factory = new IDBFactory();
      await seedLineage(factory, "active", minimalSnapshot(), activeKey);
      const incoming = await seedLineage(
        factory,
        "incoming",
        largeSnapshot(),
        incomingKey,
      );
      expect(incoming.records).toHaveLength(2);
      const bytes = new Uint8Array(incoming.descriptor.byteLength);
      for (const record of incoming.records) {
        bytes.set(record.bytes, record.index * lineageRecordSize);
      }
      const boundary = lineageRecordSize + delta;
      await mutate(factory, lineageRecordsStoreName, (transaction) => {
        const records = transaction.objectStore(lineageRecordsStoreName);
        records.delete([incomingKey, 0]);
        records.delete([incomingKey, 1]);
        records.put({
          storageKey: incomingKey,
          index: 0,
          bytes: bytes.slice(0, boundary),
        });
        records.put({
          storageKey: incomingKey,
          index: 1,
          bytes: bytes.slice(boundary),
        });
      });

      await expectCorrupt(factory);
    },
  );

  it.each(["not valid JSON", JSON.stringify({})])(
    "rejects invalid active document %s",
    async (serialized) => {
      const factory = new IDBFactory();
      await seedLineage(factory, "active", serialized, activeKey);
      await expectCorrupt(factory);
    },
  );
});

async function lineageState(factory: IDBFactory): Promise<{
  readonly metadata: LineageDescriptor[];
  readonly records: LineageRecord[];
}> {
  const database = await openSnapshotDatabase(factory);
  const transaction = database.transaction(
    [lineageMetadataStoreName, lineageRecordsStoreName],
    "readonly",
  );
  const done = transactionDone(transaction);
  const metadata = transaction
    .objectStore(lineageMetadataStoreName)
    .getAll() as IDBRequest<LineageDescriptor[]>;
  const records = transaction
    .objectStore(lineageRecordsStoreName)
    .getAll() as IDBRequest<LineageRecord[]>;
  await done;
  database.close();
  return { metadata: metadata.result, records: records.result };
}

function recordsFor(
  state: { readonly records: readonly LineageRecord[] },
  storageKey: string,
): readonly LineageRecord[] {
  return state.records.filter((record) => record.storageKey === storageKey);
}

function fixedCrypto(
  storageKey: string,
): Pick<Crypto, "randomUUID" | "subtle"> {
  return {
    randomUUID: () =>
      storageKey as `${string}-${string}-${string}-${string}-${string}`,
    subtle: crypto.subtle,
  };
}

async function expectExactReason(
  promise: Promise<unknown>,
  reason: unknown,
): Promise<void> {
  try {
    await promise;
    throw new Error("expected cancellation");
  } catch (cause) {
    expect(cause).toBe(reason);
  }
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}

async function expectCorrupt(factory: IDBFactory): Promise<void> {
  await expect(loadActiveSnapshot(factory, crypto)).rejects.toMatchObject({
    kind: "corrupt",
  });
}

async function seedLineage(
  factory: IDBFactory,
  slot: LineageSlot,
  serialized: string,
  storageKey: string,
): Promise<EncodedLineage> {
  const encoded = await encodeLineageRecords(
    slot,
    storageKey,
    serialized,
    crypto,
  );
  const database = await openSnapshotDatabase(factory);
  const transaction = database.transaction(
    [lineageMetadataStoreName, lineageRecordsStoreName],
    "readwrite",
  );
  const done = transactionDone(transaction);
  transaction.objectStore(lineageMetadataStoreName).put(encoded.descriptor);
  for (const record of encoded.records) {
    transaction.objectStore(lineageRecordsStoreName).put(record);
  }
  await done;
  database.close();
  return encoded;
}

async function mutate(
  factory: IDBFactory,
  stores: string | string[],
  operation: (transaction: IDBTransaction) => void,
): Promise<void> {
  const database = await openSnapshotDatabase(factory);
  const transaction = database.transaction(stores, "readwrite");
  const done = transactionDone(transaction);
  operation(transaction);
  await done;
  database.close();
}

async function corruptUnknownSlot(factory: IDBFactory): Promise<void> {
  const encoded = await encodeLineageRecords(
    "active",
    activeKey,
    minimalSnapshot(),
    crypto,
  );
  await mutate(
    factory,
    [lineageMetadataStoreName, lineageRecordsStoreName],
    (transaction) => {
      transaction
        .objectStore(lineageMetadataStoreName)
        .put({ ...encoded.descriptor, slot: "third" });
      for (const record of encoded.records) {
        transaction.objectStore(lineageRecordsStoreName).put(record);
      }
    },
  );
}

async function corruptThirdDescriptor(factory: IDBFactory): Promise<void> {
  await seedLineage(factory, "active", minimalSnapshot(), activeKey);
  await seedLineage(factory, "incoming", minimalSnapshot(), incomingKey);
  await mutate(factory, lineageMetadataStoreName, (transaction) => {
    transaction.objectStore(lineageMetadataStoreName).put({
      slot: "third",
      storageKey: "33333333-3333-4333-8333-333333333333",
      recordCount: 1,
      byteLength: 1,
      sha256: "0".repeat(64),
    });
  });
}

async function corruptDuplicateOwnership(factory: IDBFactory): Promise<void> {
  const active = await seedLineage(
    factory,
    "active",
    minimalSnapshot(),
    activeKey,
  );
  await mutate(factory, lineageMetadataStoreName, (transaction) => {
    transaction
      .objectStore(lineageMetadataStoreName)
      .put({ ...active.descriptor, slot: "incoming" });
  });
}

async function corruptOrphanRecord(factory: IDBFactory): Promise<void> {
  await openAndClose(factory);
  await mutate(factory, lineageRecordsStoreName, (transaction) => {
    transaction.objectStore(lineageRecordsStoreName).put({
      storageKey: "33333333-3333-4333-8333-333333333333",
      index: 0,
      bytes: new Uint8Array([1]),
    });
  });
}

async function corruptMalformedRecordKey(factory: IDBFactory): Promise<void> {
  const encoded = await seedLineage(
    factory,
    "active",
    minimalSnapshot(),
    activeKey,
  );
  await mutate(factory, lineageRecordsStoreName, (transaction) => {
    transaction.objectStore(lineageRecordsStoreName).delete([activeKey, 0]);
    transaction.objectStore(lineageRecordsStoreName).put({
      ...encoded.records[0],
      index: "0",
    });
  });
}

async function corruptMissingRecord(factory: IDBFactory): Promise<void> {
  const encoded = await seedLineage(
    factory,
    "active",
    largeSnapshot(),
    activeKey,
  );
  expect(encoded.records.length).toBeGreaterThan(1);
  await mutate(factory, lineageRecordsStoreName, (transaction) => {
    transaction.objectStore(lineageRecordsStoreName).delete([activeKey, 1]);
  });
}

async function corruptExtraRecord(factory: IDBFactory): Promise<void> {
  const encoded = await seedLineage(
    factory,
    "active",
    minimalSnapshot(),
    activeKey,
  );
  await mutate(factory, lineageRecordsStoreName, (transaction) => {
    transaction.objectStore(lineageRecordsStoreName).put({
      storageKey: activeKey,
      index: encoded.descriptor.recordCount,
      bytes: new Uint8Array([1]),
    });
  });
}

async function corruptIncomingMetadata(factory: IDBFactory): Promise<void> {
  await seedLineage(factory, "active", minimalSnapshot(), activeKey);
  await mutate(factory, lineageMetadataStoreName, (transaction) => {
    transaction.objectStore(lineageMetadataStoreName).put({
      slot: "incoming",
      storageKey: incomingKey,
      recordCount: 1,
      byteLength: 1,
      sha256: "0".repeat(64),
      unexpected: true,
    });
  });
}

async function corruptActiveRecordValue(factory: IDBFactory): Promise<void> {
  await seedLineage(factory, "active", minimalSnapshot(), activeKey);
  await mutate(factory, lineageRecordsStoreName, (transaction) => {
    transaction.objectStore(lineageRecordsStoreName).put({
      storageKey: activeKey,
      index: 0,
      bytes: "not bytes",
    });
  });
}

async function corruptActiveDigest(factory: IDBFactory): Promise<void> {
  const encoded = await seedLineage(
    factory,
    "active",
    minimalSnapshot(),
    activeKey,
  );
  await mutate(factory, lineageMetadataStoreName, (transaction) => {
    transaction
      .objectStore(lineageMetadataStoreName)
      .put({ ...encoded.descriptor, sha256: "0".repeat(64) });
  });
}

async function corruptActiveUtf8(factory: IDBFactory): Promise<void> {
  const bytes = new Uint8Array([0xff]);
  const sha256 = Array.from(
    new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)),
    (byte) => byte.toString(16).padStart(2, "0"),
  ).join("");
  await openAndClose(factory);
  await mutate(
    factory,
    [lineageMetadataStoreName, lineageRecordsStoreName],
    (transaction) => {
      transaction.objectStore(lineageMetadataStoreName).put({
        slot: "active",
        storageKey: activeKey,
        recordCount: 1,
        byteLength: 1,
        sha256,
      });
      transaction.objectStore(lineageRecordsStoreName).put({
        storageKey: activeKey,
        index: 0,
        bytes,
      });
    },
  );
}

async function openAndClose(factory: IDBFactory): Promise<void> {
  const database = await openSnapshotDatabase(factory);
  database.close();
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}

function rawOpen(factory: IDBFactory, version: number): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = factory.open(snapshotDatabaseName, version);
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

function rawSchema(
  factory: IDBFactory,
  create: (database: IDBDatabase) => void,
): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = factory.open(snapshotDatabaseName, 1);
    request.onupgradeneeded = () => create(request.result);
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

function minimalSnapshot(): string {
  return JSON.stringify({
    schemaVersion: 1,
    lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
    observedAt: "2026-07-26T12:00:00Z",
    boards: { state: "failed" },
  });
}

function olderSameIdSnapshot(): string {
  return JSON.stringify({
    schemaVersion: 1,
    lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
    observedAt: "2026-07-25T12:00:00Z",
    boards: { state: "failed" },
  });
}

function largeSnapshot(): string {
  return JSON.stringify({
    schemaVersion: 1,
    lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
    observedAt: "2026-07-26T12:00:00Z",
    boards: {
      state: "present",
      items: [{ board: { text: "x".repeat(lineageRecordSize) } }],
    },
  });
}

function maximumCardinalitySnapshot() {
  return {
    schemaVersion: 1,
    lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
    observedAt: "2026-07-26T12:00:00Z",
    boards: {
      state: "present",
      items: [
        {
          board: { board: "g" },
          catalog: {
            state: "present",
            pages: [
              {
                metadata: { page: 1 },
                threads: Array.from({ length: 250 }, (_, threadIndex) => ({
                  summary: { no: threadIndex },
                  thread: {
                    state: "present",
                    posts: Array.from({ length: 250 }, (_, postIndex) => ({
                      no: threadIndex * 1_000 + postIndex,
                    })),
                  },
                })),
              },
            ],
          },
        },
      ],
    },
  };
}
