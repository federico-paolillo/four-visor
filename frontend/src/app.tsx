// This module defines the local-storage application states and their Preact composition.

import type { ComponentChildren } from "preact";

import { BoardCatalog } from "./board-catalog";
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
import {
  SnapshotSynchronizationError,
  type SnapshotSynchronizationErrorKind,
} from "./snapshot-sync";

export type ApplicationState =
  | { readonly kind: "loading" }
  | { readonly kind: "ready"; readonly snapshot: SnapshotV1 }
  | { readonly kind: "empty" }
  | { readonly kind: "synchronizing"; readonly snapshot?: SnapshotV1 }
  | {
      readonly kind: "synchronization-error";
      readonly snapshot?: SnapshotV1;
      readonly error: ApplicationSynchronizationFailure;
    }
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

export type ApplicationSynchronizationFailure =
  | SnapshotSynchronizationError
  | { readonly kind: "canceled"; readonly cause: unknown };

export type ApplicationController = {
  readonly synchronizeWhenDue: (signal: AbortSignal) => Promise<void>;
};

export type ApplicationDependencies = {
  readonly confirm: (message: string) => boolean;
  readonly load: () => Promise<SnapshotV1 | undefined>;
  readonly render: ApplicationRenderer;
  readonly reset: (
    reportProgress: (progress: LocalResetProgress) => void,
  ) => Promise<void>;
  readonly synchronize: (signal: AbortSignal) => Promise<SnapshotV1>;
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
    case "synchronizing":
      return {
        heading: "Synchronizing snapshot",
        message:
          state.snapshot === undefined
            ? "4Visor is downloading and validating the first complete snapshot."
            : `Snapshot ${state.snapshot.lineageId} remains available while its replacement is downloaded and validated.`,
        reset: "disabled",
        resetLabel: "Synchronizing…",
      };
    case "synchronization-error":
      return synchronizationErrorPresentation(
        state.error.kind,
        state.snapshot !== undefined,
      );
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
  const snapshot = visibleSnapshot(state);

  if (snapshot !== undefined) {
    return (
      <main class="min-h-screen bg-slate-950 px-4 py-6 text-slate-100 sm:px-6 sm:py-8">
        <div class="mx-auto w-full max-w-7xl">
          <header class="sm:flex sm:items-start sm:justify-between sm:gap-6">
            <div>
              <p class="text-sm font-semibold uppercase tracking-[0.2em] text-cyan-400">
                Read-only · Anonymous
              </p>
              <h1 class="mt-2 text-3xl font-bold tracking-tight sm:text-4xl">
                4Visor
              </h1>
            </div>
            {presentation.reset !== "hidden" && (
              <button
                class="mt-4 rounded-lg bg-cyan-500 px-4 py-2 font-semibold text-slate-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400 disabled:cursor-wait disabled:opacity-50 sm:mt-0"
                disabled={presentation.reset === "disabled"}
                onClick={() => void onReset()}
                type="button"
              >
                {presentation.resetLabel}
              </button>
            )}
          </header>
          {state.kind !== "ready" && (
            <section
              class="mt-6 rounded-xl border border-slate-700 bg-slate-900 p-4"
              role={presentation.role}
            >
              <Status heading={presentation.heading}>
                {presentation.message}
              </Status>
            </section>
          )}
          <BoardCatalog key={snapshot} snapshot={snapshot} />
        </div>
      </main>
    );
  }

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
            class="mt-6 rounded-lg bg-cyan-500 px-4 py-2 font-semibold text-slate-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400 disabled:cursor-wait disabled:opacity-50"
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

function visibleSnapshot(state: ApplicationState): SnapshotV1 | undefined {
  switch (state.kind) {
    case "ready":
    case "synchronizing":
    case "synchronization-error":
      return state.snapshot;
    default:
      return undefined;
  }
}

// startApplication runs the exact production startup and reset state machine.
export async function startApplication(
  dependencies: ApplicationDependencies,
): Promise<ApplicationController> {
  let state: ApplicationState = { kind: "loading" };
  let currentSnapshot: SnapshotV1 | undefined;
  let inFlight: Promise<void> | undefined;
  const show = () => dependencies.render(state, requestReset);

  function synchronizeWhenDue(signal: AbortSignal): Promise<void> {
    if (inFlight !== undefined) {
      return inFlight;
    }
    if (!synchronizationAllowed(state)) {
      return Promise.resolve();
    }

    const operation = runSynchronization(signal);
    let tracked!: Promise<void>;
    tracked = operation.finally(() => {
      if (inFlight === tracked) {
        inFlight = undefined;
      }
    });
    inFlight = tracked;
    return tracked;
  }

  async function runSynchronization(signal: AbortSignal): Promise<void> {
    state = { kind: "synchronizing", snapshot: currentSnapshot };
    show();
    try {
      currentSnapshot = await dependencies.synchronize(signal);
      state = { kind: "ready", snapshot: currentSnapshot };
    } catch (cause) {
      state = {
        kind: "synchronization-error",
        snapshot: currentSnapshot,
        error: signal.aborted
          ? { kind: "canceled", cause }
          : cause instanceof SnapshotSynchronizationError
            ? cause
            : new SnapshotSynchronizationError("storage", cause),
      };
    }
    show();
  }

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
    currentSnapshot = snapshot;
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

  return { synchronizeWhenDue };
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
  return [
    "ready",
    "empty",
    "synchronization-error",
    "storage-error",
    "reset-error",
  ].includes(state.kind);
}

function synchronizationAllowed(state: ApplicationState): boolean {
  return ["ready", "empty", "synchronization-error"].includes(state.kind);
}

function synchronizationErrorPresentation(
  kind: SnapshotSynchronizationErrorKind | "canceled",
  hasSnapshot: boolean,
): ApplicationPresentation {
  const messages: Record<
    SnapshotSynchronizationErrorKind | "canceled",
    string
  > = {
    network:
      "The snapshot server is unavailable or the network connection failed.",
    gone: "The server has no complete snapshot available yet.",
    http: "The snapshot server returned an unsuccessful response.",
    "invalid-json": "The server returned an invalid snapshot document.",
    "invalid-contract":
      "The server snapshot does not match the required schema.",
    "unsupported-version":
      "The deployed frontend and backend use incompatible snapshot versions.",
    quota:
      "This device does not have enough available site storage for the replacement snapshot.",
    storage:
      "The replacement snapshot could not be stored or verified locally.",
    activation: "The replacement snapshot could not be activated atomically.",
    canceled: "Snapshot synchronization was canceled.",
  };
  return {
    heading: "Snapshot synchronization failed",
    message: `${messages[kind]} ${
      hasSnapshot
        ? "The previous complete snapshot remains available."
        : "No local snapshot is available yet."
    } A later scheduled synchronization can try again.`,
    reset: "enabled",
    resetLabel: "Reset local data",
    role: "alert",
  };
}
