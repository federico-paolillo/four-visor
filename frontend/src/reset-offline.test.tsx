// This module crosses the production entry and real worker events to prove reset restores offline shell continuity.

import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ApplicationState } from "./app";
import { type ShellCacheStorage, shellCachePrefix } from "./shell-cache";

const preact = vi.hoisted(() => ({ render: vi.fn() }));

vi.mock("preact", async (importOriginal) => ({
  ...(await importOriginal<typeof import("preact")>()),
  render: preact.render,
}));

const origin = "https://four-visor.test";
const rootScope = `${origin}/`;
const workerCacheName = "__FOURVISOR_SHELL_CACHE_NAME__";
const shellAssetsMarker = "__FOURVISOR_SHELL_ASSETS__";
const shellAssets = ["/index.html", "/assets/app.js", "/assets/app.css"];

beforeEach(() => {
  vi.resetModules();
  preact.render.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("reset to real worker offline continuity", () => {
  it("re-registers, runs the real install handler, and serves an offline navigation", async () => {
    const browser = await resetAndReload(false);
    const worker = await browser.workerLoading;
    const installWork = worker.dispatchInstall();

    await installWork;

    expect(browser.register).toHaveBeenCalledTimes(2);
    expect(browser.cache.names()).toEqual(["foreign-cache", workerCacheName]);
    expect(browser.cache.entries(workerCacheName)).toEqual(
      shellAssets.map((path) => new URL(path, origin).href),
    );
    expect(worker.skipWaiting).toHaveBeenCalledOnce();
    expect(
      await (await worker.dispatchFetch(navigationRequest("/"))).text(),
    ).toBe("cached:/index.html");
    expect(worker.networkFetch).not.toHaveBeenCalled();
  });

  it("rejects failed real installation without an installed offline shell", async () => {
    const browser = await resetAndReload(true);
    const worker = await browser.workerLoading;

    await expect(worker.dispatchInstall()).rejects.toThrow(
      "simulated precache failure",
    );

    expect(worker.skipWaiting).not.toHaveBeenCalled();
    expect(browser.cache.entries(workerCacheName)).toEqual([]);
    await expect(worker.dispatchFetch(navigationRequest("/"))).rejects.toThrow(
      "offline",
    );
  });
});

async function resetAndReload(failInstallation: boolean) {
  const cache = memoryCacheStorage([`${shellCachePrefix}old`, "foreign-cache"]);
  let registration: ServiceWorkerRegistration | undefined;
  let workerLoading: Promise<ReturnType<typeof workerDouble>> | undefined;
  const unregister = vi.fn(async () => {
    registration = undefined;
    return true;
  });
  const rootRegistration = {
    scope: rootScope,
    unregister,
  } as unknown as ServiceWorkerRegistration;
  registration = rootRegistration;
  const register = vi.fn(async () => {
    registration = rootRegistration;
    if (register.mock.calls.length === 2) {
      cache.failInstallation = failInstallation;
      workerLoading = loadRealWorker(cache.storage);
    }
    return rootRegistration;
  });
  let reloadedApplication: Promise<unknown> | undefined;
  const reload = vi.fn(() => {
    vi.resetModules();
    reloadedApplication = import("./index");
  });

  installBrowserGlobals({
    cache: cache.storage,
    factory: new IDBFactory(),
    reload,
    serviceWorkers: {
      getRegistration: vi.fn(async () => registration),
      register,
    },
  });

  await import("./index");
  await vi.waitFor(() => expect(latestState().kind).toBe("empty"));
  await latestReset()();
  expect(unregister).toHaveBeenCalledOnce();
  expect(reload).toHaveBeenCalledOnce();
  expect(cache.names()).toEqual(["foreign-cache"]);

  await reloadedApplication;
  if (workerLoading === undefined) {
    throw new Error("reload did not execute production worker registration");
  }

  return { cache, register, workerLoading };
}

function installBrowserGlobals({
  cache,
  factory,
  reload,
  serviceWorkers,
}: {
  readonly cache: ShellCacheStorage;
  readonly factory: IDBFactory;
  readonly reload: () => void;
  readonly serviceWorkers: Pick<
    ServiceWorkerContainer,
    "getRegistration" | "register"
  >;
}) {
  vi.stubEnv("PROD", true);
  vi.stubGlobal("caches", cache);
  vi.stubGlobal("crypto", crypto);
  vi.stubGlobal("document", { getElementById: vi.fn(() => ({})) });
  vi.stubGlobal("fetch", vi.fn());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
  vi.stubGlobal("indexedDB", factory);
  vi.stubGlobal("location", { href: rootScope, reload });
  vi.stubGlobal("navigator", { serviceWorker: serviceWorkers });
  vi.stubGlobal("window", { confirm: vi.fn(() => true) });
}

async function loadRealWorker(cacheStorage: ShellCacheStorage) {
  const worker = workerDouble(cacheStorage);
  vi.stubGlobal("self", worker.global);
  const parse = JSON.parse.bind(JSON);
  const parseSpy = vi
    .spyOn(JSON, "parse")
    .mockImplementation((text, reviver) =>
      text === shellAssetsMarker ? shellAssets : parse(text, reviver),
    );
  await import("./service-worker");
  parseSpy.mockRestore();
  return worker;
}

type WorkerEvent = {
  readonly request?: Request;
  respondWith?(response: Promise<Response>): void;
  waitUntil?(work: Promise<void>): void;
};

function workerDouble(cacheStorage: ShellCacheStorage) {
  const handlers = new Map<string, (event: WorkerEvent) => void>();
  const networkFetch = vi.fn(async () => {
    throw new TypeError("offline");
  });
  const skipWaiting = vi.fn(async () => {});

  return {
    global: {
      caches: cacheStorage,
      clients: { claim: vi.fn(async () => {}) },
      fetch: networkFetch,
      location: { origin },
      skipWaiting,
      addEventListener(type: string, handler: (event: WorkerEvent) => void) {
        handlers.set(type, handler);
      },
    },
    networkFetch,
    skipWaiting,
    dispatchInstall() {
      let work: Promise<void> | undefined;
      dispatch(handlers, "install", {
        waitUntil(candidate) {
          work = candidate;
        },
      });
      if (work === undefined) {
        throw new Error("install handler did not provide waitUntil work");
      }
      return work;
    },
    dispatchFetch(request: Request) {
      let response: Promise<Response> | undefined;
      dispatch(handlers, "fetch", {
        request,
        respondWith(candidate) {
          response = candidate;
        },
      });
      if (response === undefined) {
        throw new Error("fetch handler did not provide respondWith work");
      }
      return response;
    },
  };
}

function dispatch(
  handlers: ReadonlyMap<string, (event: WorkerEvent) => void>,
  type: string,
  event: WorkerEvent,
): void {
  const handler = handlers.get(type);
  if (handler === undefined) {
    throw new Error(`missing ${type} handler`);
  }
  handler(event);
}

function memoryCacheStorage(initial: readonly string[]) {
  const caches = new Map<string, Map<string, Response>>(
    initial.map((name) => [name, new Map()]),
  );
  let failInstallation = false;
  const storage: ShellCacheStorage = {
    async delete(name) {
      return caches.delete(name);
    },
    async keys() {
      return [...caches.keys()];
    },
    async open(name) {
      let entries = caches.get(name);
      if (entries === undefined) {
        entries = new Map();
        caches.set(name, entries);
      }
      return {
        async addAll(paths) {
          const staged = new Map<string, Response>();
          for (const path of paths) {
            if (failInstallation) {
              throw new TypeError("simulated precache failure");
            }
            const value = String(path);
            staged.set(
              new URL(value, origin).href,
              new Response(`cached:${value}`),
            );
          }
          for (const [path, response] of staged) {
            entries.set(path, response);
          }
        },
        async match(request) {
          return entries.get(new URL(String(request), origin).href)?.clone();
        },
      };
    },
  };

  return {
    storage,
    entries: (name: string) => [...(caches.get(name)?.keys() ?? [])],
    names: () => [...caches.keys()],
    set failInstallation(value: boolean) {
      failInstallation = value;
    },
  };
}

function navigationRequest(path: string): Request {
  return {
    method: "GET",
    mode: "navigate",
    url: new URL(path, origin).href,
  } as Request;
}

function latestState(): ApplicationState {
  const call = preact.render.mock.calls[preact.render.mock.calls.length - 1];
  if (call === undefined) {
    throw new Error("expected production entry to render");
  }
  return (call[0] as { props: { state: ApplicationState } }).props.state;
}

function latestReset(): () => Promise<void> {
  const call = preact.render.mock.calls[preact.render.mock.calls.length - 1];
  if (call === undefined) {
    throw new Error("expected production entry to render");
  }
  return (call[0] as { props: { onReset: () => Promise<void> } }).props.onReset;
}
