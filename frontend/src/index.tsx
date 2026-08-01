// This module composes the browser storage application and registers its production worker.

import { render } from "preact";

import { App, startApplication } from "./app";
import {
  loadOrCreateClientRefreshJitter,
  runClientRefresh,
} from "./client-refresh";
import { resetLocalData } from "./local-reset";
import { loadActiveSnapshot, replaceActiveSnapshot } from "./snapshot-storage";
import { synchronizeSnapshot } from "./snapshot-sync";
import "./style.css";

const appEntryPoint = document.getElementById("app");
if (appEntryPoint === null) {
  throw new Error("application mount point is missing");
}

const rootScope = new URL("/", location.href).href;
let appRegistration: Promise<unknown> = Promise.resolve();
let clientRefreshJitter: number | undefined;
const clientRefresh = new AbortController();

export const applicationController = startApplication({
  confirm: (message) => window.confirm(message),
  load: async () => {
    const snapshot = await loadActiveSnapshot(indexedDB, crypto);
    clientRefreshJitter = await loadOrCreateClientRefreshJitter(
      indexedDB,
      crypto,
    );
    return snapshot;
  },
  render: (state, reset) =>
    render(<App state={state} onReset={reset} />, appEntryPoint),
  reset: async (reportProgress) => {
    clientRefresh.abort();
    await resetLocalData(
      indexedDB,
      caches,
      "serviceWorker" in navigator ? navigator.serviceWorker : undefined,
      rootScope,
      () => location.reload(),
      reportProgress,
      appRegistration,
    );
  },
  synchronize: (signal) =>
    synchronizeSnapshot(signal, fetch, (serialized, ownerSignal) =>
      replaceActiveSnapshot(
        serialized,
        ownerSignal,
        indexedDB,
        IDBKeyRange,
        crypto,
      ),
    ),
});

void applicationController.then((controller) => {
  if (clientRefreshJitter !== undefined) {
    void runClientRefresh(
      clientRefreshJitter,
      controller.synchronizeWhenDue,
      clientRefresh.signal,
      navigator.locks,
    );
  }
});

if (import.meta.env.PROD && "serviceWorker" in navigator) {
  appRegistration = navigator.serviceWorker
    .register("/service-worker.js", {
      scope: "/",
      type: "module",
      updateViaCache: "none",
    })
    .catch((cause: unknown) => {
      console.error("Service Worker registration failed", cause);
    });
}
