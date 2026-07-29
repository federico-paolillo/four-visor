// This module defines the local-storage application states and their Preact composition.

import type { ComponentChildren } from "preact";

import {
  LocalResetError,
  type LocalResetProgress,
  resetConfirmationText,
} from "./local-reset";
import type { SnapshotV1 } from "./snapshot";
import {
  SnapshotStorageError,
  type SnapshotStorageErrorKind,
} from "./snapshot-storage";

export type ApplicationState =
  | { readonly kind: "loading" }
  | { readonly kind: "ready"; readonly snapshot: SnapshotV1 }
  | { readonly kind: "empty" }
  | {
      readonly kind: "storage-error";
      readonly error: SnapshotStorageError;
    }
  | { readonly kind: "resetting"; readonly blocked: boolean }
  | { readonly kind: "reset-error"; readonly error: LocalResetError };

export type ApplicationPresentation = {
  readonly heading: string;
  readonly message: string;
  readonly reset: "hidden" | "enabled" | "disabled";
  readonly resetLabel: string;
  readonly role?: "alert";
};

export type ApplicationRenderer = (
  state: ApplicationState,
  reset: () => Promise<void>,
) => void;

export type ApplicationDependencies = {
  readonly confirm: (message: string) => boolean;
  readonly load: () => Promise<SnapshotV1 | undefined>;
  readonly render: ApplicationRenderer;
  readonly reset: (
    reportProgress: (progress: LocalResetProgress) => void,
  ) => Promise<void>;
};

// applicationPresentation maps internal state to fixed, safe user-facing copy.
export function applicationPresentation(
  state: ApplicationState,
): ApplicationPresentation {
  switch (state.kind) {
    case "loading":
      return {
        heading: "Opening local storage",
        message: "4Visor is loading its required local snapshot storage.",
        reset: "hidden",
        resetLabel: "Reset local data",
      };
    case "ready":
      return {
        heading: "Local snapshot ready",
        message: `Snapshot ${state.snapshot.lineageId} observed at ${state.snapshot.observedAt} is available from this device.`,
        reset: "enabled",
        resetLabel: "Reset local data",
      };
    case "empty":
      return {
        heading: "No local snapshot",
        message: "No local snapshot is available on this installation.",
        reset: "enabled",
        resetLabel: "Reset local data",
      };
    case "storage-error":
      return storageErrorPresentation(state.error.kind);
    case "resetting":
      return {
        heading: state.blocked
          ? "Waiting for local storage"
          : "Resetting local data",
        message: state.blocked
          ? "Close other 4Visor tabs so this local-only reset can continue."
          : "4Visor is deleting local snapshot data and application-shell caches.",
        reset: "disabled",
        resetLabel: "Resetting…",
        role: "alert",
      };
    case "reset-error":
      return {
        heading: "Local reset incomplete",
        message:
          "Local data could not be fully reset. No server data was changed. Retry the local reset.",
        reset: "enabled",
        resetLabel: "Retry reset",
        role: "alert",
      };
  }
}

// App renders one storage state without introducing a separate state framework.
export function App({
  state,
  onReset,
}: {
  readonly state: ApplicationState;
  readonly onReset: () => Promise<void>;
}) {
  const presentation = applicationPresentation(state);
  return (
    <main class="flex min-h-screen items-center justify-center bg-slate-950 px-6 py-12 text-slate-100">
      <section
        class="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900 p-8 shadow-xl"
        role={presentation.role}
      >
        <p class="text-sm font-semibold uppercase tracking-[0.2em] text-cyan-400">
          Read-only · Anonymous
        </p>
        <h1 class="mt-3 text-4xl font-bold tracking-tight">4Visor</h1>
        <Status heading={presentation.heading}>{presentation.message}</Status>
        {presentation.reset !== "hidden" && (
          <button
            class="mt-6 rounded-lg bg-cyan-500 px-4 py-2 font-semibold text-slate-950 disabled:cursor-wait disabled:opacity-50"
            disabled={presentation.reset === "disabled"}
            onClick={() => void onReset()}
            type="button"
          >
            {presentation.resetLabel}
          </button>
        )}
      </section>
    </main>
  );
}

// startApplication runs the exact production startup and reset state machine.
export async function startApplication(
  dependencies: ApplicationDependencies,
): Promise<void> {
  let state: ApplicationState = { kind: "loading" };
  const show = () => dependencies.render(state, requestReset);

  async function requestReset(): Promise<void> {
    if (!resetAllowed(state) || !dependencies.confirm(resetConfirmationText)) {
      return;
    }

    state = { kind: "resetting", blocked: false };
    show();
    try {
      await dependencies.reset((progress) => {
        if (progress === "database-blocked") {
          state = { kind: "resetting", blocked: true };
          show();
        }
      });
    } catch (cause) {
      state = {
        kind: "reset-error",
        error:
          cause instanceof LocalResetError
            ? cause
            : new LocalResetError("reload", cause),
      };
      show();
    }
  }

  show();
  try {
    const snapshot = await dependencies.load();
    state =
      snapshot === undefined ? { kind: "empty" } : { kind: "ready", snapshot };
  } catch (cause) {
    state = {
      kind: "storage-error",
      error:
        cause instanceof SnapshotStorageError
          ? cause
          : new SnapshotStorageError("unavailable", cause),
    };
  }
  show();
}

function Status({
  heading,
  children,
}: {
  readonly heading: string;
  readonly children: ComponentChildren;
}) {
  return (
    <div class="mt-4">
      <h2 class="text-xl font-semibold">{heading}</h2>
      <p class="mt-2 text-base leading-7 text-slate-300">{children}</p>
    </div>
  );
}

function storageErrorPresentation(
  kind: SnapshotStorageErrorKind,
): ApplicationPresentation {
  return {
    heading:
      kind === "corrupt"
        ? "Local storage is corrupt"
        : "Local storage is unavailable",
    message:
      kind === "corrupt"
        ? "4Visor cannot use the stored snapshot data. Reset local data to continue."
        : "4Visor requires IndexedDB, but local browser storage is unavailable. Enable site storage and reload, or reset local data.",
    reset: "enabled",
    resetLabel: "Reset local data",
    role: "alert",
  };
}

function resetAllowed(state: ApplicationState): boolean {
  return ["ready", "empty", "storage-error", "reset-error"].includes(
    state.kind,
  );
}
