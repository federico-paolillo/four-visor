// This module renders the local application shell and registers its production worker.

import { render } from "preact";

import "./style.css";

export function App() {
  return (
    <main class="flex min-h-screen items-center justify-center bg-slate-950 px-6 py-12 text-slate-100">
      <section class="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900 p-8 shadow-xl">
        <p class="text-sm font-semibold uppercase tracking-[0.2em] text-cyan-400">
          Read-only · Anonymous
        </p>
        <h1 class="mt-3 text-4xl font-bold tracking-tight">4Visor</h1>
        <p class="mt-4 text-base leading-7 text-slate-300">
          The application shell is ready. Local snapshot content will appear
          here when it is available.
        </p>
      </section>
    </main>
  );
}

const appEntryPoint = document.getElementById("app");
if (appEntryPoint === null) {
  throw new Error("application mount point is missing");
}

render(<App />, appEntryPoint);

if (import.meta.env.PROD && "serviceWorker" in navigator) {
  void navigator.serviceWorker
    .register("/service-worker.js", {
      scope: "/",
      type: "module",
      updateViaCache: "none",
    })
    .catch((cause: unknown) => {
      console.error("Service Worker registration failed", cause);
    });
}
