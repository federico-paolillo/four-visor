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

  return {
    descriptor: {
      slot,
      storageKey,
      recordCount: records.length,
      byteLength: bytes.byteLength,
      sha256: await digest(bytes, cryptoApi),
    },
    records,
  };
}

// openSnapshotDatabase creates or opens only the exact version-1 schema.
export async function openSnapshotDatabase(
  factory: IDBFactory | undefined = globalThis.indexedDB,
): Promise<IDBDatabase> {
  if (factory === undefined) {
    throw new SnapshotStorageError("unavailable");
  }

  let request: IDBOpenDBRequest;
  try {
    request = factory.open(snapshotDatabaseName, snapshotDatabaseVersion);
  } catch (cause) {
    throw storageFailure(cause);
  }

  const database = await openedDatabase(request);
  database.onversionchange = () => database.close();

  try {
    await validateDatabaseSchema(database);
    return database;
  } catch (cause) {
    database.close();
    throw cause;
  }
}

// loadActiveSnapshot audits all lineage ownership and validates only the active payload.
export async function loadActiveSnapshot(
  factory: IDBFactory | undefined = globalThis.indexedDB,
  keyRange: typeof IDBKeyRange = globalThis.IDBKeyRange,
  cryptoApi: Pick<Crypto, "subtle"> = globalThis.crypto,
): Promise<SnapshotV1 | undefined> {
  let database: IDBDatabase | undefined;
  try {
    database = await openSnapshotDatabase(factory);
    const descriptors = await auditLineageOwnership(database);
    const active = descriptors.get("active");
    if (active === undefined) {
      return undefined;
    }

    return await readActiveLineage(database, active, keyRange, cryptoApi);
  } catch (cause) {
    if (cause instanceof SnapshotStorageError) {
      throw cause;
    }
    throw storageFailure(cause);
  } finally {
    database?.close();
  }
}

function openedDatabase(request: IDBOpenDBRequest): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    let settled = false;
    let upgradeFailure: SnapshotStorageError | undefined;

    request.onupgradeneeded = (event) => {
      if (event.oldVersion !== 0) {
        upgradeFailure = new SnapshotStorageError("corrupt");
        request.transaction?.abort();
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
        request.transaction?.abort();
      }
    };
    request.onblocked = () => {
      if (!settled) {
        settled = true;
        reject(new SnapshotStorageError("unavailable"));
      }
    };
    request.onerror = () => {
      if (!settled) {
        settled = true;
        reject(upgradeFailure ?? storageFailure(request.error));
      }
    };
    request.onsuccess = () => {
      if (settled) {
        request.result.close();
        return;
      }
      settled = true;
      resolve(request.result);
    };
  });
}

async function validateDatabaseSchema(database: IDBDatabase): Promise<void> {
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
  const completion = transactionCompletion(transaction);

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
    throw storageFailure(cause);
  }
}

async function auditLineageOwnership(
  database: IDBDatabase,
): Promise<ReadonlyMap<LineageSlot, LineageDescriptor>> {
  let transaction: IDBTransaction;
  try {
    transaction = database.transaction(
      [lineageMetadataStoreName, lineageRecordsStoreName],
      "readonly",
    );
  } catch (cause) {
    throw storageFailure(cause);
  }
  const completion = transactionCompletion(transaction);
  const metadataRequest = transaction
    .objectStore(lineageMetadataStoreName)
    .getAll();
  const recordsRequest = transaction
    .objectStore(lineageRecordsStoreName)
    .getAll();

  try {
    await completion;
  } catch (cause) {
    throw storageFailure(cause);
  }

  try {
    const descriptors = parseDescriptors(metadataRequest.result);
    validateRecords(recordsRequest.result, descriptors);
    return descriptors;
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

async function readActiveLineage(
  database: IDBDatabase,
  descriptor: LineageDescriptor,
  keyRanges: typeof IDBKeyRange,
  cryptoApi: Pick<Crypto, "subtle">,
): Promise<SnapshotV1> {
  let transaction: IDBTransaction;
  let request: IDBRequest<LineageRecord[]>;
  try {
    transaction = database.transaction(lineageRecordsStoreName, "readonly");
    const completion = transactionCompletion(transaction);
    request = transaction
      .objectStore(lineageRecordsStoreName)
      .getAll(
        keyRanges.bound(
          [descriptor.storageKey, 0],
          [descriptor.storageKey, descriptor.recordCount - 1],
        ),
      ) as IDBRequest<LineageRecord[]>;
    await completion;
  } catch (cause) {
    throw storageFailure(cause);
  }

  try {
    const bytes = reassemble(descriptor, request.result);
    if ((await digest(bytes, cryptoApi)) !== descriptor.sha256) {
      throw new SnapshotStorageError("corrupt");
    }
    return parseSnapshot(
      new TextDecoder("utf-8", { fatal: true }).decode(bytes),
    );
  } catch (cause) {
    if (cause instanceof SnapshotStorageError) {
      throw cause;
    }
    throw new SnapshotStorageError("corrupt", cause);
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

function transactionCompletion(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    let cause: unknown;
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => {
      cause = transaction.error;
    };
    transaction.onabort = () => reject(cause ?? transaction.error);
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
