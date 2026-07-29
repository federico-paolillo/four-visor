// This module composes the browser storage application and registers its production worker.

import { render } from "preact";

import { App, startApplication } from "./app";
import { resetLocalData } from "./local-reset";
import { loadActiveSnapshot } from "./snapshot-storage";
import "./style.css";

const appEntryPoint = document.getElementById("app");
if (appEntryPoint === null) {
  throw new Error("application mount point is missing");
}

const rootScope = new URL("/", location.href).href;
let appRegistration: Promise<unknown> = Promise.resolve();

void startApplication({
  confirm: (message) => window.confirm(message),
  load: () => loadActiveSnapshot(indexedDB, IDBKeyRange, crypto),
  render: (state, reset) =>
    render(<App state={state} onReset={reset} />, appEntryPoint),
  reset: (reportProgress) =>
    resetLocalData(
      indexedDB,
      caches,
      "serviceWorker" in navigator ? navigator.serviceWorker : undefined,
      rootScope,
      () => location.reload(),
      reportProgress,
      appRegistration,
    ),
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
