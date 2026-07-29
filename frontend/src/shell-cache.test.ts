// This module proves shell-cache policy and lifecycle behavior with browser API doubles.

import { describe, expect, it, vi } from "vitest";

import {
  activateShell,
  fetchShell,
  installShell,
  obsoleteShellCacheNames,
  ownedShellCacheNames,
  type ShellCacheStorage,
  shellCachePrefix,
  shellRequestKey,
} from "./shell-cache";

const origin = "https://four-visor.example";
const currentCache = `${shellCachePrefix}current`;
const shellAssets = [
  "/index.html",
  "/assets/app-abc123.js",
  "/assets/app-def456.css",
  "/manifest.webmanifest",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
] as const;
const shellAssetPaths = new Set(shellAssets);

describe("shell request allowlist", () => {
  it.each([
    ["index", request("/index.html"), `${origin}/index.html`],
    [
      "script",
      request("/assets/app-abc123.js"),
      `${origin}/assets/app-abc123.js`,
    ],
    [
      "style",
      request("/assets/app-def456.css"),
      `${origin}/assets/app-def456.css`,
    ],
    [
      "manifest",
      request("/manifest.webmanifest"),
      `${origin}/manifest.webmanifest`,
    ],
  ])("allows exact same-origin GET %s requests", (_, candidate, expected) => {
    expect(shellRequestKey(candidate, origin, shellAssetPaths)).toBe(expected);
  });

  it.each([
    ["query variant", request("/index.html?lineage=1")],
    ["API snapshot", request("/api/snapshot")],
    ["snapshot JSON", request("/snapshot.json")],
    ["arbitrary path", request("/boards/g")],
    ["cross-origin media", new Request("https://i.4cdn.org/g/1.jpg")],
    ["non-GET", request("/index.html", { method: "POST" })],
  ])("bypasses %s", (_, candidate) => {
    expect(shellRequestKey(candidate, origin, shellAssetPaths)).toBeUndefined();
  });

  it("maps only root and index navigations to the cached index", () => {
    expect(
      shellRequestKey(navigationRequest("/"), origin, shellAssetPaths),
    ).toBe(`${origin}/index.html`);
    expect(
      shellRequestKey(
        navigationRequest("/index.html"),
        origin,
        shellAssetPaths,
      ),
    ).toBe(`${origin}/index.html`);
    expect(
      shellRequestKey(navigationRequest("/boards/g"), origin, shellAssetPaths),
    ).toBeUndefined();
  });
});

describe("shell cache cleanup", () => {
  it("selects every owned cache for local reset", () => {
    expect(
      ownedShellCacheNames([
        `${shellCachePrefix}old-a`,
        "another-application-cache",
        currentCache,
      ]),
    ).toEqual([`${shellCachePrefix}old-a`, currentCache]);
  });

  it("selects only obsolete owned shell caches", () => {
    expect(
      obsoleteShellCacheNames(
        [
          `${shellCachePrefix}old-a`,
          "another-application-cache",
          currentCache,
          `${shellCachePrefix}old-b`,
        ],
        currentCache,
      ),
    ).toEqual([`${shellCachePrefix}old-a`, `${shellCachePrefix}old-b`]);
  });
});

describe("shell Service Worker lifecycle", () => {
  it("precaches every shell asset before skipping waiting", async () => {
    const double = memoryCacheStorage();
    const events: string[] = [];

    await installShell(double.storage, currentCache, shellAssets, async () => {
      events.push("skipWaiting");
    });

    expect(double.keys()).toEqual([currentCache]);
    expect(double.entries(currentCache)).toEqual(
      shellAssets.map((path) => new URL(path, origin).href),
    );
    expect(events).toEqual(["skipWaiting"]);
  });

  it("rejects installation atomically without skipping waiting", async () => {
    const double = memoryCacheStorage("/assets/app-def456.css");
    const skipWaiting = vi.fn();

    await expect(
      installShell(double.storage, currentCache, shellAssets, skipWaiting),
    ).rejects.toThrow("simulated precache failure");

    expect(double.entries(currentCache)).toEqual([]);
    expect(skipWaiting).not.toHaveBeenCalled();
  });

  it("deletes obsolete owned caches and claims only after cleanup", async () => {
    const double = memoryCacheStorage();
    double.seed(`${shellCachePrefix}old`, "/index.html");
    double.seed(currentCache, "/index.html");
    double.seed("foreign-cache", "/unrelated.js");

    await activateShell(double.storage, currentCache, async () => {
      expect(double.keys()).toEqual([currentCache, "foreign-cache"]);
    });

    expect(double.deleted).toEqual([`${shellCachePrefix}old`]);
  });

  it("does not claim clients when cleanup fails", async () => {
    const double = memoryCacheStorage();
    double.seed(`${shellCachePrefix}old`, "/index.html");
    double.failDeletion = true;
    const claimClients = vi.fn();

    await expect(
      activateShell(double.storage, currentCache, claimClients),
    ).rejects.toThrow("simulated cleanup failure");
    expect(claimClients).not.toHaveBeenCalled();
  });

  it("serves installed shell assets while the network is offline", async () => {
    const double = memoryCacheStorage();
    await installShell(
      double.storage,
      currentCache,
      shellAssets,
      async () => {},
    );
    const offlineFetch = vi.fn(async () => {
      throw new TypeError("offline");
    });

    for (const path of ["/index.html", "/assets/app-abc123.js"] as const) {
      const response = await fetchShell(
        request(path),
        double.storage,
        currentCache,
        origin,
        shellAssetPaths,
        offlineFetch,
      );
      expect(await response?.text()).toBe(path);
    }
    const navigationResponse = await fetchShell(
      navigationRequest("/"),
      double.storage,
      currentCache,
      origin,
      shellAssetPaths,
      offlineFetch,
    );
    expect(await navigationResponse?.text()).toBe("/index.html");
    expect(offlineFetch).not.toHaveBeenCalled();
  });

  it("bypasses excluded traffic without touching cache or network", () => {
    const double = memoryCacheStorage();
    const networkFetch = vi.fn();

    for (const candidate of [
      request("/api/snapshot"),
      request("/snapshot.json"),
      request("/index.html?refresh=1"),
      new Request("https://i.4cdn.org/g/1.jpg"),
      request("/index.html", { method: "POST" }),
    ]) {
      expect(
        fetchShell(
          candidate,
          double.storage,
          currentCache,
          origin,
          shellAssetPaths,
          networkFetch,
        ),
      ).toBeUndefined();
    }

    expect(double.opened).toEqual([]);
    expect(networkFetch).not.toHaveBeenCalled();
  });

  it("uses but never runtime-caches a network response on shell-cache miss", async () => {
    const double = memoryCacheStorage();
    const networkFetch = vi.fn(async () => new Response("network"));

    const response = await fetchShell(
      request("/index.html"),
      double.storage,
      currentCache,
      origin,
      shellAssetPaths,
      networkFetch,
    );

    expect(await response?.text()).toBe("network");
    expect(networkFetch).toHaveBeenCalledOnce();
    expect(double.entries(currentCache)).toEqual([]);
  });
});

function request(path: string, init?: RequestInit): Request {
  return new Request(new URL(path, origin), init);
}

function navigationRequest(path: string): Request {
  return {
    method: "GET",
    mode: "navigate",
    url: new URL(path, origin).href,
  } as Request;
}

function memoryCacheStorage(failingAsset?: string) {
  const caches = new Map<string, Map<string, Response>>();
  const opened: string[] = [];
  const deleted: string[] = [];
  let failDeletion = false;

  const storage: ShellCacheStorage = {
    async open(cacheName) {
      opened.push(cacheName);
      let entries = caches.get(cacheName);
      if (entries === undefined) {
        entries = new Map();
        caches.set(cacheName, entries);
      }
      return {
        async addAll(paths) {
          const staged = new Map<string, Response>();
          for (const path of paths) {
            if (path === failingAsset) {
              throw new TypeError("simulated precache failure");
            }
            staged.set(
              new URL(String(path), origin).href,
              new Response(String(path)),
            );
          }
          for (const [path, response] of staged) {
            entries.set(path, response);
          }
        },
        async match(candidate) {
          return entries.get(new URL(String(candidate), origin).href)?.clone();
        },
      };
    },
    async keys() {
      return [...caches.keys()];
    },
    async delete(cacheName) {
      if (failDeletion) {
        throw new Error("simulated cleanup failure");
      }
      deleted.push(cacheName);
      return caches.delete(cacheName);
    },
  };

  return {
    deleted,
    opened,
    storage,
    entries(cacheName: string) {
      return [...(caches.get(cacheName)?.keys() ?? [])];
    },
    keys() {
      return [...caches.keys()];
    },
    seed(cacheName: string, path: string) {
      caches.set(
        cacheName,
        new Map([[new URL(path, origin).href, new Response(path)]]),
      );
    },
    get failDeletion() {
      return failDeletion;
    },
    set failDeletion(value: boolean) {
      failDeletion = value;
    },
  };
}
