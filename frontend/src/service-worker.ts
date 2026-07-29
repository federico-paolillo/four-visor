/// <reference lib="webworker" />

// This module wires the generated shell configuration to browser Service Worker events.

import { activateShell, fetchShell, installShell } from "./shell-cache";

declare const self: ServiceWorkerGlobalScope;

const shellCacheName = "__FOURVISOR_SHELL_CACHE_NAME__";
const shellAssets = JSON.parse(
  "__FOURVISOR_SHELL_ASSETS__",
) as readonly string[];
const shellAssetPaths = new Set(shellAssets);

self.addEventListener("install", (event) => {
  event.waitUntil(
    installShell(self.caches, shellCacheName, shellAssets, () =>
      self.skipWaiting(),
    ),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    activateShell(self.caches, shellCacheName, () => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const response = fetchShell(
    event.request,
    self.caches,
    shellCacheName,
    self.location.origin,
    shellAssetPaths,
    self.fetch.bind(self),
  );
  if (response !== undefined) {
    event.respondWith(response);
  }
});
