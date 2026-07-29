// This module verifies the Service Worker entry point wires browser events to shell-cache promises.

import { afterEach, describe, expect, it, vi } from "vitest";

import { type ShellCacheStorage, shellCachePrefix } from "./shell-cache";

const origin = "https://four-visor.example";
const cacheName = "__FOURVISOR_SHELL_CACHE_NAME__";
const shellAssetsMarker = "__FOURVISOR_SHELL_ASSETS__";
const shellAssets = [
  "/index.html",
  "/assets/app-abc123.js",
  "/assets/app-def456.css",
] as const;

type WorkerEvent = {
  readonly request?: Request;
  respondWith?(response: Promise<Response>): void;
  waitUntil?(work: Promise<void>): void;
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.resetModules();
});

describe("Service Worker event wiring", () => {
  it("passes install and activate work to waitUntil in lifecycle order", async () => {
    const worker = await loadWorker();
    const installWaitUntil = vi.fn<(work: Promise<void>) => void>();

    worker.dispatch("install", { waitUntil: installWaitUntil });

    expect(installWaitUntil).toHaveBeenCalledOnce();
    const installWork = installWaitUntil.mock.calls[0]?.[0];
    expect(installWork).toBeInstanceOf(Promise);
    await installWork;
    expect(worker.addAll).toHaveBeenCalledWith(shellAssets);
    expect(worker.events).toEqual([
      `open:${cacheName}`,
      "addAll:start",
      "addAll:done",
      "skipWaiting",
    ]);

    worker.events.length = 0;
    const activateWaitUntil = vi.fn<(work: Promise<void>) => void>();

    worker.dispatch("activate", { waitUntil: activateWaitUntil });

    expect(activateWaitUntil).toHaveBeenCalledOnce();
    const activateWork = activateWaitUntil.mock.calls[0]?.[0];
    expect(activateWork).toBeInstanceOf(Promise);
    await activateWork;
    expect(worker.events).toEqual([
      "keys",
      `delete:start:${shellCachePrefix}old`,
      `delete:done:${shellCachePrefix}old`,
      "claim",
    ]);
  });

  it("passes allowed fetch work to respondWith and leaves bypasses alone", async () => {
    const worker = await loadWorker();
    const respondWith = vi.fn<(response: Promise<Response>) => void>();

    worker.dispatch("fetch", {
      request: new Request(`${origin}/index.html`),
      respondWith,
    });

    expect(respondWith).toHaveBeenCalledOnce();
    const responseWork = respondWith.mock.calls[0]?.[0];
    expect(responseWork).toBeInstanceOf(Promise);
    expect(await (await responseWork)?.text()).toBe("cached index");
    expect(worker.networkFetch).not.toHaveBeenCalled();

    for (const request of [
      new Request(`${origin}/api/snapshot`),
      new Request(`${origin}/index.html?refresh=1`),
      new Request(`${origin}/boards/g`),
      new Request("https://i.4cdn.org/g/1.jpg"),
      new Request(`${origin}/index.html`, { method: "POST" }),
    ]) {
      const bypassRespondWith = vi.fn();
      worker.dispatch("fetch", { request, respondWith: bypassRespondWith });
      expect(bypassRespondWith).not.toHaveBeenCalled();
    }
    expect(worker.networkFetch).not.toHaveBeenCalled();
  });
});

async function loadWorker() {
  vi.resetModules();
  const events: string[] = [];
  const handlers = new Map<string, (event: WorkerEvent) => void>();
  const addAll = vi.fn(async () => {
    events.push("addAll:start");
    await Promise.resolve();
    events.push("addAll:done");
  });
  const cache = {
    addAll,
    async match(key: string) {
      events.push(`match:${key}`);
      return key === `${origin}/index.html`
        ? new Response("cached index")
        : undefined;
    },
  };
  const storage: ShellCacheStorage = {
    async open(name) {
      events.push(`open:${name}`);
      return cache;
    },
    async keys() {
      events.push("keys");
      return [`${shellCachePrefix}old`, cacheName, "foreign-cache"];
    },
    async delete(name) {
      events.push(`delete:start:${name}`);
      await Promise.resolve();
      events.push(`delete:done:${name}`);
      return true;
    },
  };
  const networkFetch = vi.fn(async () => new Response("network"));
  vi.stubGlobal("self", {
    caches: storage,
    clients: {
      claim: async () => {
        events.push("claim");
      },
    },
    fetch: networkFetch,
    location: { origin },
    skipWaiting: async () => {
      events.push("skipWaiting");
    },
    addEventListener(type: string, handler: (event: WorkerEvent) => void) {
      handlers.set(type, handler);
    },
  });

  const parse = JSON.parse.bind(JSON);
  const parseSpy = vi
    .spyOn(JSON, "parse")
    .mockImplementation((text, reviver) =>
      text === shellAssetsMarker ? shellAssets : parse(text, reviver),
    );
  await import("./service-worker");
  parseSpy.mockRestore();

  return {
    addAll,
    events,
    networkFetch,
    dispatch(type: string, event: WorkerEvent) {
      const handler = handlers.get(type);
      if (handler === undefined) {
        throw new Error(`missing ${type} handler`);
      }
      handler(event);
    },
  };
}
