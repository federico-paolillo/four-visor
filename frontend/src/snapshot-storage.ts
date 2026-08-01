// This module owns the mandatory IndexedDB schema, lineage codec, and startup read boundary.

import { parseSnapshot, type SnapshotV1 } from "./snapshot";

export const snapshotDatabaseName = "four-visor-snapshots";
export const snapshotDatabaseVersion = 1;
export const lineageMetadataStoreName = "lineage-metadata";
export const lineageRecordsStoreName = "lineage-records";
export const settingsStoreName = "settings";
export const jitterSeedKey = "jitter-seed";
export const lineageRecordSize = 65_536;

export type LineageSlot = "active" | "incoming";

export type LineageDescriptor = {
  readonly slot: LineageSlot;
  readonly storageKey: string;
  readonly recordCount: number;
  readonly byteLength: number;
  readonly sha256: string;
};

export type LineageRecord = {
  readonly storageKey: string;
  readonly index: number;
  readonly bytes: Uint8Array;
};

export type EncodedLineage = {
  readonly descriptor: LineageDescriptor;
  readonly records: readonly LineageRecord[];
};

type LineageInventory = {
  readonly descriptors: ReadonlyMap<LineageSlot, LineageDescriptor>;
  readonly records: readonly LineageRecord[];
};

export type SnapshotStorageErrorKind = "unavailable" | "corrupt";

// SnapshotStorageError classifies mandatory-storage failures without exposing stored data.
export class SnapshotStorageError extends Error {
  readonly cause: unknown;
  readonly kind: SnapshotStorageErrorKind;

  constructor(kind: SnapshotStorageErrorKind, cause?: unknown) {
    super(
      kind === "corrupt"
        ? "local snapshot storage is corrupt"
        : "local snapshot storage is unavailable",
    );
    this.name = "SnapshotStorageError";
    this.kind = kind;
    this.cause = cause;
  }
}

export type SnapshotReplacementErrorKind = "quota" | "storage" | "activation";

// SnapshotReplacementError classifies refresh storage failures without exposing browser diagnostics.
export class SnapshotReplacementError extends Error {
  readonly cause: unknown;
  readonly kind: SnapshotReplacementErrorKind;

  constructor(kind: SnapshotReplacementErrorKind, cause?: unknown) {
    super(`snapshot replacement ${kind} failure`);
    this.name = "SnapshotReplacementError";
    this.kind = kind;
    this.cause = cause;
  }
}

// createLocalStorageKey creates lineage ownership independently of upstream metadata.
export function createLocalStorageKey(
  cryptoApi: Pick<Crypto, "randomUUID"> = globalThis.crypto,
): string {
  return cryptoApi.randomUUID();
}

// encodeLineageRecords serializes one opaque document into fixed-size local records.
export async function encodeLineageRecords(
  slot: LineageSlot,
  storageKey: string,
  serialized: string,
  cryptoApi: Pick<Crypto, "subtle"> = globalThis.crypto,
  signal?: AbortSignal,
): Promise<EncodedLineage> {
  if (!validSlot(slot) || !validStorageKey(storageKey)) {
    throw new SnapshotStorageError("corrupt");
  }

  const bytes = new TextEncoder().encode(serialized);
  if (bytes.byteLength === 0) {
    throw new SnapshotStorageError("corrupt");
  }

  const records: LineageRecord[] = [];
  for (let offset = 0; offset < bytes.byteLength; offset += lineageRecordSize) {
    records.push({
      storageKey,
      index: records.length,
      bytes: bytes.slice(offset, offset + lineageRecordSize),
    });
  }

  signal?.throwIfAborted();
  const sha256 = await digest(bytes, cryptoApi);
  signal?.throwIfAborted();

  return {
    descriptor: {
      slot,
      storageKey,
      recordCount: records.length,
      byteLength: bytes.byteLength,
      sha256,
    },
    records,
  };
}

// openSnapshotDatabase creates or opens only the exact version-1 schema.
export async function openSnapshotDatabase(
  factory: IDBFactory | undefined = globalThis.indexedDB,
  signal?: AbortSignal,
): Promise<IDBDatabase> {
  signal?.throwIfAborted();
  if (factory === undefined) {
    throw new SnapshotStorageError("unavailable");
  }

  let request: IDBOpenDBRequest;
  try {
    request = factory.open(snapshotDatabaseName, snapshotDatabaseVersion);
  } catch (cause) {
    signal?.throwIfAborted();
    throw storageFailure(cause);
  }

  const database = await openedDatabase(request, signal);
  database.onversionchange = () => database.close();

  try {
    signal?.throwIfAborted();
    await validateDatabaseSchema(database, signal);
    signal?.throwIfAborted();
    return database;
  } catch (cause) {
    database.close();
    signal?.throwIfAborted();
    throw cause;
  }
}

// loadActiveSnapshot audits all lineage ownership and validates only the active payload.
export async function loadActiveSnapshot(
  factory: IDBFactory | undefined = globalThis.indexedDB,
  cryptoApi: Pick<Crypto, "subtle"> = globalThis.crypto,
): Promise<SnapshotV1 | undefined> {
  let database: IDBDatabase | undefined;
  try {
    database = await openSnapshotDatabase(factory);
    const inventory = await auditLineageOwnership(database);
    const active = inventory.descriptors.get("active");
    if (active === undefined) {
      return undefined;
    }

    return await decodeStoredLineage(
      active,
      inventory.records.filter(
        ({ storageKey }) => storageKey === active.storageKey,
      ),
      cryptoApi,
    );
  } catch (cause) {
    if (cause instanceof SnapshotStorageError) {
      throw cause;
    }
    throw new SnapshotStorageError("corrupt", cause);
  } finally {
    database?.close();
  }
}

// replaceActiveSnapshot stages, validates, and atomically promotes one complete document.
export async function replaceActiveSnapshot(
  serialized: string,
  signal: AbortSignal,
  factory: IDBFactory | undefined = globalThis.indexedDB,
  keyRange: typeof IDBKeyRange = globalThis.IDBKeyRange,
  cryptoApi: Pick<Crypto, "randomUUID" | "subtle"> = globalThis.crypto,
): Promise<SnapshotV1> {
  signal.throwIfAborted();

  let database: IDBDatabase | undefined;
  try {
    database = await openSnapshotDatabase(factory, signal);
    signal.throwIfAborted();

    const storageKey = createLocalStorageKey(cryptoApi);
    signal.throwIfAborted();
    const encoded = await encodeLineageRecords(
      "incoming",
      storageKey,
      serialized,
      cryptoApi,
      signal,
    );
    signal.throwIfAborted();

    await stageIncomingLineage(database, encoded, keyRange, signal);
    signal.throwIfAborted();
    const snapshot = await validateIncomingLineage(
      database,
      storageKey,
      cryptoApi,
      signal,
    );
    signal.throwIfAborted();
    await activateIncomingLineage(database, storageKey, keyRange, signal);

    return snapshot;
  } catch (cause) {
    signal.throwIfAborted();
    if (cause instanceof SnapshotReplacementError) {
      throw cause;
    }
    if (cause instanceof SnapshotStorageError) {
      throw replacementFailure("storage", cause);
    }
    throw cause;
  } finally {
    database?.close();
  }
}

async function stageIncomingLineage(
  database: IDBDatabase,
  encoded: EncodedLineage,
  keyRange: typeof IDBKeyRange,
  signal: AbortSignal,
): Promise<void> {
  signal.throwIfAborted();

  let transaction: IDBTransaction;
  try {
    transaction = database.transaction(
      [lineageMetadataStoreName, lineageRecordsStoreName],
      "readwrite",
    );
  } catch (cause) {
    signal.throwIfAborted();
    throw replacementFailure("storage", cause);
  }

  let schedulingFailure: unknown;
  const completion = transactionCompletion(transaction, signal);
  const metadata = transaction.objectStore(lineageMetadataStoreName);
  const records = transaction.objectStore(lineageRecordsStoreName);
  const incomingRequest = metadata.get("incoming");
  incomingRequest.onsuccess = () => {
    try {
      if (incomingRequest.result !== undefined) {
        const previous = parseDescriptor(incomingRequest.result, "incoming");
        records.delete(recordRange(previous, keyRange));
      }
      for (const record of encoded.records) {
        records.put(record);
      }
      metadata.put(encoded.descriptor);
    } catch (cause) {
      schedulingFailure = cause;
      transaction.abort();
    }
  };

  try {
    await completion;
    signal.throwIfAborted();
  } catch (cause) {
    signal.throwIfAborted();
    throw replacementFailure("storage", schedulingFailure ?? cause);
  }
}

async function validateIncomingLineage(
  database: IDBDatabase,
  expectedStorageKey: string,
  cryptoApi: Pick<Crypto, "subtle">,
  signal: AbortSignal,
): Promise<SnapshotV1> {
  signal.throwIfAborted();

  let inventory: LineageInventory;
  try {
    inventory = await auditLineageOwnership(database, signal);
    signal.throwIfAborted();
  } catch (cause) {
    signal.throwIfAborted();
    throw replacementFailure("storage", cause);
  }

  const incoming = inventory.descriptors.get("incoming");
  if (incoming?.storageKey !== expectedStorageKey) {
    throw replacementFailure(
      "storage",
      new Error("staged incoming lineage changed"),
    );
  }

  try {
    return await decodeStoredLineage(
      incoming,
      inventory.records.filter(
        ({ storageKey }) => storageKey === incoming.storageKey,
      ),
      cryptoApi,
      signal,
    );
  } catch (cause) {
    signal.throwIfAborted();
    if (cause instanceof SnapshotStorageError) {
      throw replacementFailure("storage", cause);
    }
    throw cause;
  }
}

async function activateIncomingLineage(
  database: IDBDatabase,
  expectedStorageKey: string,
  keyRange: typeof IDBKeyRange,
  signal: AbortSignal,
): Promise<void> {
  signal.throwIfAborted();

  let transaction: IDBTransaction;
  try {
    transaction = database.transaction(
      [lineageMetadataStoreName, lineageRecordsStoreName],
      "readwrite",
    );
  } catch (cause) {
    signal.throwIfAborted();
    throw replacementFailure("activation", cause);
  }

  let schedulingFailure: unknown;
  const completion = transactionCompletion(transaction, signal, true);
  const metadata = transaction.objectStore(lineageMetadataStoreName);
  const records = transaction.objectStore(lineageRecordsStoreName);
  const activeRequest = metadata.get("active");
  const incomingRequest = metadata.get("incoming");
  let activeReady = false;
  let incomingReady = false;

  const schedulePromotion = () => {
    if (!activeReady || !incomingReady) {
      return;
    }
    try {
      const active =
        activeRequest.result === undefined
          ? undefined
          : parseDescriptor(activeRequest.result, "active");
      const incoming = parseDescriptor(incomingRequest.result, "incoming");
      if (incoming.storageKey !== expectedStorageKey) {
        throw new Error("validated incoming lineage changed");
      }

      metadata.put({ ...incoming, slot: "active" });
      metadata.delete("incoming");
      if (active !== undefined) {
        records.delete(recordRange(active, keyRange));
      }
    } catch (cause) {
      schedulingFailure = cause;
      transaction.abort();
    }
  };

  activeRequest.onsuccess = () => {
    activeReady = true;
    schedulePromotion();
  };
  incomingRequest.onsuccess = () => {
    incomingReady = true;
    schedulePromotion();
  };

  try {
    await completion;
  } catch (cause) {
    signal.throwIfAborted();
    throw replacementFailure("activation", schedulingFailure ?? cause);
  }
}

function openedDatabase(
  request: IDBOpenDBRequest,
  signal?: AbortSignal,
): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    let settled = false;
    let canceled = false;
    let upgradeActive = false;
    let upgradeFailure: SnapshotStorageError | undefined;

    const removeAbortListener = () =>
      signal?.removeEventListener("abort", onCancel);
    const rejectOnce = (cause: unknown) => {
      if (!settled) {
        settled = true;
        removeAbortListener();
        reject(cause);
      }
    };
    const abortUpgrade = () => {
      if (!upgradeActive || request.transaction === null) {
        return;
      }
      try {
        request.transaction.abort();
      } catch (cause) {
        if (!errorNamed(cause, "InvalidStateError")) {
          upgradeFailure = new SnapshotStorageError("unavailable", cause);
        }
      }
    };
    const onCancel = () => {
      canceled = true;
      abortUpgrade();
      try {
        signal?.throwIfAborted();
      } catch (cause) {
        rejectOnce(cause);
      }
    };

    signal?.addEventListener("abort", onCancel, { once: true });
    if (signal?.aborted) {
      onCancel();
    }

    request.onupgradeneeded = (event) => {
      const transaction = request.transaction;
      if (transaction !== null) {
        upgradeActive = true;
        const finishUpgrade = () => {
          upgradeActive = false;
          transaction.removeEventListener("complete", finishUpgrade);
          transaction.removeEventListener("abort", finishUpgrade);
        };
        transaction.addEventListener("complete", finishUpgrade);
        transaction.addEventListener("abort", finishUpgrade);
      }
      if (canceled || signal?.aborted) {
        abortUpgrade();
        return;
      }
      if (event.oldVersion !== 0) {
        upgradeFailure = new SnapshotStorageError("corrupt");
        abortUpgrade();
        return;
      }

      try {
        const database = request.result;
        database.createObjectStore(lineageMetadataStoreName, {
          keyPath: "slot",
        });
        database.createObjectStore(lineageRecordsStoreName, {
          keyPath: ["storageKey", "index"],
        });
        database.createObjectStore(settingsStoreName);
      } catch (cause) {
        upgradeFailure = new SnapshotStorageError("corrupt", cause);
        abortUpgrade();
      }
    };
    request.onblocked = () => {
      if (!canceled) {
        rejectOnce(new SnapshotStorageError("unavailable"));
      }
    };
    request.onerror = () => {
      removeAbortListener();
      if (!canceled) {
        rejectOnce(upgradeFailure ?? storageFailure(request.error));
      }
    };
    request.onsuccess = () => {
      removeAbortListener();
      if (settled || canceled) {
        request.result.close();
        return;
      }
      settled = true;
      resolve(request.result);
    };
  });
}

async function validateDatabaseSchema(
  database: IDBDatabase,
  signal?: AbortSignal,
): Promise<void> {
  const expectedStores = [
    lineageMetadataStoreName,
    lineageRecordsStoreName,
    settingsStoreName,
  ];
  if (!sameNames(database.objectStoreNames, expectedStores)) {
    throw new SnapshotStorageError("corrupt");
  }

  let transaction: IDBTransaction;
  try {
    transaction = database.transaction(expectedStores, "readonly");
  } catch (cause) {
    throw storageFailure(cause);
  }
  const completion = transactionCompletion(transaction, signal);

  const metadata = transaction.objectStore(lineageMetadataStoreName);
  const records = transaction.objectStore(lineageRecordsStoreName);
  const settings = transaction.objectStore(settingsStoreName);
  const invalid =
    metadata.keyPath !== "slot" ||
    !compoundKeyPath(records.keyPath, ["storageKey", "index"]) ||
    settings.keyPath !== null ||
    metadata.autoIncrement ||
    records.autoIncrement ||
    settings.autoIncrement ||
    metadata.indexNames.length !== 0 ||
    records.indexNames.length !== 0 ||
    settings.indexNames.length !== 0;
  if (invalid) {
    transaction.abort();
    try {
      await completion;
    } catch {
      // The explicit abort only drains the invalid schema inspection transaction.
    }
    throw new SnapshotStorageError("corrupt");
  }

  try {
    await completion;
  } catch (cause) {
    signal?.throwIfAborted();
    throw storageFailure(cause);
  }
}

async function auditLineageOwnership(
  database: IDBDatabase,
  signal?: AbortSignal,
): Promise<LineageInventory> {
  let transaction: IDBTransaction;
  try {
    transaction = database.transaction(
      [lineageMetadataStoreName, lineageRecordsStoreName],
      "readonly",
    );
  } catch (cause) {
    throw storageFailure(cause);
  }
  const completion = transactionCompletion(transaction, signal);
  const metadataRequest = transaction
    .objectStore(lineageMetadataStoreName)
    .getAll();
  const recordsRequest = transaction
    .objectStore(lineageRecordsStoreName)
    .getAll();

  try {
    await completion;
  } catch (cause) {
    signal?.throwIfAborted();
    throw storageFailure(cause);
  }

  try {
    const descriptors = parseDescriptors(metadataRequest.result);
    validateRecords(recordsRequest.result, descriptors);
    return {
      descriptors,
      records: recordsRequest.result as LineageRecord[],
    };
  } catch (cause) {
    if (cause instanceof SnapshotStorageError) {
      throw cause;
    }
    throw new SnapshotStorageError("corrupt", cause);
  }
}

function parseDescriptors(
  values: readonly unknown[],
): Map<LineageSlot, LineageDescriptor> {
  if (values.length > 2) {
    throw new SnapshotStorageError("corrupt");
  }

  const descriptors = new Map<LineageSlot, LineageDescriptor>();
  const storageKeys = new Set<string>();
  for (const value of values) {
    if (!exactObject(value, descriptorFields)) {
      throw new SnapshotStorageError("corrupt");
    }

    const descriptor = value as LineageDescriptor;
    if (
      !validSlot(descriptor.slot) ||
      !validStorageKey(descriptor.storageKey) ||
      !positiveInteger(descriptor.recordCount) ||
      !positiveInteger(descriptor.byteLength) ||
      Math.ceil(descriptor.byteLength / lineageRecordSize) !==
        descriptor.recordCount ||
      !/^[0-9a-f]{64}$/.test(descriptor.sha256) ||
      descriptors.has(descriptor.slot) ||
      storageKeys.has(descriptor.storageKey)
    ) {
      throw new SnapshotStorageError("corrupt");
    }

    descriptors.set(descriptor.slot, descriptor);
    storageKeys.add(descriptor.storageKey);
  }
  return descriptors;
}

function validateRecords(
  values: readonly unknown[],
  descriptors: ReadonlyMap<LineageSlot, LineageDescriptor>,
): void {
  const owners = new Map(
    [...descriptors.values()].map((descriptor) => [
      descriptor.storageKey,
      descriptor,
    ]),
  );
  const byStorageKey = new Map<string, number[]>();
  for (const value of values) {
    if (
      !exactObject(value, recordFields) ||
      typeof value.storageKey !== "string" ||
      !Number.isSafeInteger(value.index) ||
      Number(value.index) < 0 ||
      !(value.bytes instanceof Uint8Array)
    ) {
      throw new SnapshotStorageError("corrupt");
    }

    const descriptor = owners.get(value.storageKey);
    const index = Number(value.index);
    if (descriptor === undefined || index >= descriptor.recordCount) {
      throw new SnapshotStorageError("corrupt");
    }
    const expectedLength =
      index === descriptor.recordCount - 1
        ? descriptor.byteLength - lineageRecordSize * index
        : lineageRecordSize;
    if (value.bytes.byteLength !== expectedLength) {
      throw new SnapshotStorageError("corrupt");
    }

    const indexes = byStorageKey.get(value.storageKey) ?? [];
    indexes.push(index);
    byStorageKey.set(value.storageKey, indexes);
  }

  for (const descriptor of owners.values()) {
    const indexes = byStorageKey.get(descriptor.storageKey) ?? [];
    if (indexes.length !== descriptor.recordCount) {
      throw new SnapshotStorageError("corrupt");
    }
    indexes.sort((left, right) => left - right);
    if (indexes.some((index, expected) => index !== expected)) {
      throw new SnapshotStorageError("corrupt");
    }
  }
}

async function decodeStoredLineage(
  descriptor: LineageDescriptor,
  records: readonly LineageRecord[],
  cryptoApi: Pick<Crypto, "subtle">,
  signal?: AbortSignal,
): Promise<SnapshotV1> {
  signal?.throwIfAborted();
  const bytes = reassemble(descriptor, records);
  signal?.throwIfAborted();
  if ((await digest(bytes, cryptoApi)) !== descriptor.sha256) {
    signal?.throwIfAborted();
    throw new SnapshotStorageError("corrupt");
  }
  signal?.throwIfAborted();

  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch (cause) {
    signal?.throwIfAborted();
    throw new SnapshotStorageError("corrupt", cause);
  }
  signal?.throwIfAborted();

  try {
    const snapshot = parseSnapshot(text);
    signal?.throwIfAborted();
    return snapshot;
  } catch (cause) {
    signal?.throwIfAborted();
    throw cause;
  }
}

function reassemble(
  descriptor: LineageDescriptor,
  records: readonly LineageRecord[],
): Uint8Array {
  if (records.length !== descriptor.recordCount) {
    throw new SnapshotStorageError("corrupt");
  }

  const result = new Uint8Array(descriptor.byteLength);
  let offset = 0;
  for (const [expectedIndex, record] of records.entries()) {
    if (
      !exactObject(record, recordFields) ||
      record.storageKey !== descriptor.storageKey ||
      record.index !== expectedIndex ||
      !(record.bytes instanceof Uint8Array)
    ) {
      throw new SnapshotStorageError("corrupt");
    }

    const expectedLength =
      expectedIndex === records.length - 1
        ? descriptor.byteLength - lineageRecordSize * expectedIndex
        : lineageRecordSize;
    if (record.bytes.byteLength !== expectedLength) {
      throw new SnapshotStorageError("corrupt");
    }
    result.set(record.bytes, offset);
    offset += record.bytes.byteLength;
  }
  if (offset !== descriptor.byteLength) {
    throw new SnapshotStorageError("corrupt");
  }
  return result;
}

function transactionCompletion(
  transaction: IDBTransaction,
  signal?: AbortSignal,
  committedTransactionWinsCancellation = false,
): Promise<void> {
  return new Promise((resolve, reject) => {
    let cause: unknown;
    let active = true;
    let commitAlreadyWon = false;

    const cleanup = () => {
      active = false;
      transaction.removeEventListener("complete", onComplete);
      transaction.removeEventListener("error", onError);
      transaction.removeEventListener("abort", onAbort);
      signal?.removeEventListener("abort", onCancel);
    };
    const rejectCancellation = () => {
      try {
        signal?.throwIfAborted();
      } catch (reason) {
        reject(reason);
      }
    };
    const onComplete = () => {
      cleanup();
      if (
        signal?.aborted &&
        !(committedTransactionWinsCancellation && commitAlreadyWon)
      ) {
        rejectCancellation();
        return;
      }
      resolve();
    };
    const onError = () => {
      cause = transaction.error;
    };
    const onAbort = () => {
      cleanup();
      if (signal?.aborted) {
        rejectCancellation();
        return;
      }
      reject(cause ?? transaction.error);
    };
    const onCancel = () => {
      if (!active) {
        return;
      }
      try {
        // Cancellation prevents activation only while this transaction can still be aborted.
        transaction.abort();
      } catch (abortFailure) {
        if (errorNamed(abortFailure, "InvalidStateError")) {
          commitAlreadyWon = true;
          return;
        }
        cause = abortFailure;
      }
    };

    transaction.addEventListener("complete", onComplete);
    transaction.addEventListener("error", onError);
    transaction.addEventListener("abort", onAbort);
    signal?.addEventListener("abort", onCancel, { once: true });
    if (signal?.aborted) {
      onCancel();
    }
  });
}

async function digest(
  bytes: Uint8Array,
  cryptoApi: Pick<Crypto, "subtle">,
): Promise<string> {
  const hash = await cryptoApi.subtle.digest("SHA-256", bytes as BufferSource);
  return Array.from(new Uint8Array(hash), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

function storageFailure(cause: unknown): SnapshotStorageError {
  const name = cause instanceof DOMException ? cause.name : undefined;
  return new SnapshotStorageError(
    name === "VersionError" || name === "NotReadableError"
      ? "corrupt"
      : "unavailable",
    cause,
  );
}

function replacementFailure(
  stage: "storage" | "activation",
  cause: unknown,
): SnapshotReplacementError {
  return new SnapshotReplacementError(
    errorNamed(cause, "QuotaExceededError") ? "quota" : stage,
    cause,
  );
}

function parseDescriptor(
  value: unknown,
  expectedSlot: LineageSlot,
): LineageDescriptor {
  const descriptor = parseDescriptors([value]).get(expectedSlot);
  if (descriptor === undefined) {
    throw new SnapshotStorageError("corrupt");
  }
  return descriptor;
}

function recordRange(
  descriptor: LineageDescriptor,
  keyRange: typeof IDBKeyRange,
): IDBKeyRange {
  return keyRange.bound(
    [descriptor.storageKey, 0],
    [descriptor.storageKey, descriptor.recordCount - 1],
  );
}

function errorNamed(cause: unknown, name: string): boolean {
  if (cause instanceof DOMException && cause.name === name) {
    return true;
  }
  if (
    typeof cause === "object" &&
    cause !== null &&
    "cause" in cause &&
    cause.cause !== cause
  ) {
    return errorNamed(cause.cause, name);
  }
  return false;
}

function exactObject(
  value: unknown,
  fields: readonly string[],
): value is Record<string, unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    Object.keys(value).length === fields.length &&
    fields.every(
      (field) => Object.getOwnPropertyDescriptor(value, field) !== undefined,
    )
  );
}

function sameNames(names: DOMStringList, expected: readonly string[]): boolean {
  return (
    names.length === expected.length &&
    expected.every((name) => names.contains(name))
  );
}

function compoundKeyPath(
  value: string | string[] | null,
  expected: readonly string[],
): boolean {
  return (
    Array.isArray(value) &&
    value.length === expected.length &&
    value.every((field, index) => field === expected[index])
  );
}

function validSlot(value: unknown): value is LineageSlot {
  return value === "active" || value === "incoming";
}

function validStorageKey(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    )
  );
}

function positiveInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0;
}

const descriptorFields = [
  "slot",
  "storageKey",
  "recordCount",
  "byteLength",
  "sha256",
] as const;
const recordFields = ["storageKey", "index", "bytes"] as const;
