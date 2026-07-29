// This module proves the production browser entry wires real storage, reset, and worker APIs.

import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ApplicationState } from "./app";
import { resetConfirmationText } from "./local-reset";
import { shellCachePrefix } from "./shell-cache";
import {
  encodeLineageRecords,
  lineageMetadataStoreName,
  lineageRecordsStoreName,
  openSnapshotDatabase,
  snapshotDatabaseName,
} from "./snapshot-storage";

const preact = vi.hoisted(() => ({ render: vi.fn() }));

vi.mock("preact", async (importOriginal) => ({
  ...(await importOriginal<typeof import("preact")>()),
  render: preact.render,
}));

const origin = "https://four-visor.test/";

beforeEach(() => {
  vi.resetModules();
  preact.render.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("production browser entry", () => {
  it("renders loading then ready through the real loader and wires reset globally", async () => {
    const factory = new IDBFactory();
    await seedActive(factory);
    const open = vi.spyOn(factory, "open");
    const browser = installBrowser(factory, true);

    await import("./index");

    expect(renderedStates()[0]?.kind).toBe("loading");
    await vi.waitFor(() => expect(latestState().kind).toBe("ready"));
    expect(open).toHaveBeenCalledWith(snapshotDatabaseName, 1);
    expect(browser.fetch).not.toHaveBeenCalled();
    expect(browser.register).toHaveBeenCalledWith("/service-worker.js", {
      scope: "/",
      type: "module",
      updateViaCache: "none",
    });
    expect(preact.render.mock.invocationCallOrder[0]).toBeLessThan(
      browser.register.mock.invocationCallOrder[0] ?? 0,
    );

    await latestReset()();

    expect(browser.confirm).toHaveBeenCalledWith(resetConfirmationText);
    expect(browser.cacheNames()).toEqual(["foreign-cache"]);
    expect(browser.unregister).toHaveBeenCalledOnce();
    expect(browser.reload).toHaveBeenCalledOnce();
    expect(await factory.databases()).not.toContainEqual(
      expect.objectContaining({ name: snapshotDatabaseName }),
    );
    expect(browser.fetch).not.toHaveBeenCalled();
  });

  it("renders empty and skips Service Worker registration outside production", async () => {
    const browser = installBrowser(new IDBFactory(), false);

    await import("./index");

    expect(renderedStates()[0]?.kind).toBe("loading");
    await vi.waitFor(() => expect(latestState().kind).toBe("empty"));
    expect(browser.register).not.toHaveBeenCalled();
    expect(browser.fetch).not.toHaveBeenCalled();
  });

  it("exports a due-only controller that coalesces one real snapshot request", async () => {
    const browser = installBrowser(new IDBFactory(), false);
    browser.fetch.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          schemaVersion: 1,
          lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
          observedAt: "2026-07-26T12:00:00Z",
          boards: { state: "failed" },
        }),
      ),
    );

    const entry = await import("./index");
    expect(renderedStates()[0]?.kind).toBe("loading");
    expect(browser.fetch).not.toHaveBeenCalled();
    const controller = await entry.applicationController;
    expect(latestState().kind).toBe("empty");

    const owner = new AbortController();
    const ignored = new AbortController();
    const first = controller.synchronizeWhenDue(owner.signal);
    const second = controller.synchronizeWhenDue(ignored.signal);

    expect(second).toBe(first);
    await first;
    expect(browser.fetch).toHaveBeenCalledOnce();
    expect(browser.fetch).toHaveBeenCalledWith("/api/snapshot", {
      cache: "no-store",
      method: "GET",
      signal: owner.signal,
    });
    expect(latestState()).toMatchObject({
      kind: "ready",
      snapshot: { lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z" },
    });
  });

  it("waits for its pending production registration before reset cleanup", async () => {
    const pending = deferred<ServiceWorkerRegistration>();
    const factory = new IDBFactory();
    const browser = installBrowser(factory, true);
    browser.register.mockReturnValueOnce(pending.promise);

    await import("./index");
    await vi.waitFor(() => expect(latestState().kind).toBe("empty"));

    const reset = latestReset()();
    await vi.waitFor(async () =>
      expect(await factory.databases()).not.toContainEqual(
        expect.objectContaining({ name: snapshotDatabaseName }),
      ),
    );
    await Promise.resolve();
    expect(browser.cacheNames()).toContain(`${shellCachePrefix}current`);
    expect(browser.unregister).not.toHaveBeenCalled();
    expect(browser.reload).not.toHaveBeenCalled();

    pending.resolve(browser.registration);
    await reset;
    expect(browser.cacheNames()).toEqual(["foreign-cache"]);
    expect(browser.unregister).toHaveBeenCalledOnce();
    expect(browser.reload).toHaveBeenCalledOnce();
  });

  it("keeps startup available when production registration fails", async () => {
    const cause = new Error("private registration detail");
    const browser = installBrowser(new IDBFactory(), true);
    browser.register.mockRejectedValueOnce(cause);
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});

    await import("./index");
    await vi.waitFor(() => expect(latestState().kind).toBe("empty"));

    expect(consoleError).toHaveBeenCalledWith(
      "Service Worker registration failed",
      cause,
    );
    expect(latestState()).toEqual({ kind: "empty" });
  });

  it.each([
    [
      "unavailable",
      async () =>
        ({
          open() {
            throw new DOMException("private detail", "SecurityError");
          },
        }) as unknown as IDBFactory,
    ],
    ["corrupt", corruptFactory],
  ] as const)(
    "renders a blocking %s state without fetching",
    async (kind, makeFactory) => {
      const factory = await makeFactory();
      const browser = installBrowser(factory, true);

      await import("./index");

      expect(renderedStates()[0]?.kind).toBe("loading");
      await vi.waitFor(() => expect(latestState().kind).toBe("storage-error"));
      expect(latestState()).toMatchObject({
        error: { kind },
        kind: "storage-error",
      });
      expect(browser.fetch).not.toHaveBeenCalled();
    },
  );
});

function installBrowser(factory: IDBFactory, production: boolean) {
  const cacheNames = new Set([`${shellCachePrefix}current`, "foreign-cache"]);
  const unregister = vi.fn(async () => {
    rootRegistration = undefined;
    return true;
  });
  const registration = {
    scope: origin,
    unregister,
  } as unknown as ServiceWorkerRegistration;
  let rootRegistration: ServiceWorkerRegistration | undefined = registration;
  const register = vi.fn(async () => rootRegistration);
  const serviceWorker = {
    getRegistration: vi.fn(async () => rootRegistration),
    register,
  };
  const confirm = vi.fn(() => true);
  const fetch = vi.fn();
  const reload = vi.fn();

  vi.stubEnv("PROD", production);
  vi.stubGlobal("caches", {
    async delete(name: string) {
      return cacheNames.delete(name);
    },
    async keys() {
      return [...cacheNames];
    },
  });
  vi.stubGlobal("crypto", crypto);
  vi.stubGlobal("document", { getElementById: vi.fn(() => ({})) });
  vi.stubGlobal("fetch", fetch);
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
  vi.stubGlobal("indexedDB", factory);
  vi.stubGlobal("location", { href: origin, reload });
  vi.stubGlobal("navigator", { serviceWorker });
  vi.stubGlobal("window", { confirm });

  return {
    cacheNames: () => [...cacheNames],
    confirm,
    fetch,
    register,
    registration,
    reload,
    unregister,
  };
}

function renderedStates(): ApplicationState[] {
  return preact.render.mock.calls.map(
    ([node]) => (node as { props: { state: ApplicationState } }).props.state,
  );
}

function latestState(): ApplicationState {
  const states = renderedStates();
  const state = states[states.length - 1];
  if (state === undefined) {
    throw new Error("expected production entry to render");
  }
  return state;
}

function latestReset(): () => Promise<void> {
  const call = preact.render.mock.calls[preact.render.mock.calls.length - 1];
  if (call === undefined) {
    throw new Error("expected production entry to render");
  }
  return (call[0] as { props: { onReset: () => Promise<void> } }).props.onReset;
}

async function seedActive(factory: IDBFactory): Promise<void> {
  const encoded = await encodeLineageRecords(
    "active",
    "11111111-1111-4111-8111-111111111111",
    JSON.stringify({
      schemaVersion: 1,
      lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
      observedAt: "2026-07-26T12:00:00Z",
      boards: { state: "failed" },
    }),
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
}

function corruptFactory(): Promise<IDBFactory> {
  const factory = new IDBFactory();
  return new Promise((resolve, reject) => {
    const request = factory.open(snapshotDatabaseName, 1);
    request.onupgradeneeded = () =>
      request.result.createObjectStore("not-the-approved-schema");
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      request.result.close();
      resolve(factory);
    };
  });
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}
