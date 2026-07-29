// This module owns the exact application-shell Cache Storage boundary.

export const shellCachePrefix = "four-visor-shell-";

type ShellCache = Pick<Cache, "addAll" | "match">;

export type ShellCacheStorage = {
  delete(cacheName: string): Promise<boolean>;
  keys(): Promise<string[]>;
  open(cacheName: string): Promise<ShellCache>;
};

type ShellRequest = Pick<Request, "method" | "mode" | "url">;

export function shellRequestKey(
  request: ShellRequest,
  origin: string,
  shellAssets: ReadonlySet<string>,
): string | undefined {
  if (request.method !== "GET") {
    return undefined;
  }

  const url = new URL(request.url);
  if (url.origin !== origin || url.search !== "") {
    return undefined;
  }
  if (
    request.mode === "navigate" &&
    (url.pathname === "/" || url.pathname === "/index.html")
  ) {
    return new URL("/index.html", origin).href;
  }
  if (!shellAssets.has(url.pathname)) {
    return undefined;
  }

  return url.href;
}

export function obsoleteShellCacheNames(
  cacheNames: readonly string[],
  currentCacheName: string,
): string[] {
  return cacheNames.filter(
    (cacheName) =>
      cacheName.startsWith(shellCachePrefix) && cacheName !== currentCacheName,
  );
}

export async function installShell(
  cacheStorage: ShellCacheStorage,
  cacheName: string,
  shellAssets: readonly string[],
  skipWaiting: () => Promise<void>,
): Promise<void> {
  const cache = await cacheStorage.open(cacheName);
  await cache.addAll(shellAssets);
  await skipWaiting();
}

export async function activateShell(
  cacheStorage: ShellCacheStorage,
  currentCacheName: string,
  claimClients: () => Promise<void>,
): Promise<void> {
  const obsoleteCacheNames = obsoleteShellCacheNames(
    await cacheStorage.keys(),
    currentCacheName,
  );
  await Promise.all(
    obsoleteCacheNames.map((cacheName) => cacheStorage.delete(cacheName)),
  );
  await claimClients();
}

export function fetchShell(
  request: Request,
  cacheStorage: ShellCacheStorage,
  cacheName: string,
  origin: string,
  shellAssets: ReadonlySet<string>,
  networkFetch: typeof fetch,
): Promise<Response> | undefined {
  const cacheKey = shellRequestKey(request, origin, shellAssets);
  if (cacheKey === undefined) {
    return undefined;
  }

  return cacheStorage
    .open(cacheName)
    .then(
      async (cache) => (await cache.match(cacheKey)) ?? networkFetch(request),
    );
}
