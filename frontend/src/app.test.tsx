// This module proves the real application controller and pure storage-state presentation.

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type ApplicationDependencies,
  type ApplicationState,
  applicationPresentation,
  startApplication,
} from "./app";
import {
  LocalResetError,
  type LocalResetProgress,
  resetConfirmationText,
} from "./local-reset";
import type { SnapshotV1 } from "./snapshot";
import { SnapshotStorageError } from "./snapshot-storage";
import {
  SnapshotSynchronizationError,
  type SnapshotSynchronizationErrorKind,
} from "./snapshot-sync";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("application startup composition", () => {
  it("renders loading synchronously, then the locally ready snapshot without fetch", async () => {
    const pending = deferred<SnapshotV1 | undefined>();
    const application = applicationDouble(() => pending.promise);
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);

    const startup = startApplication(application.dependencies);

    expect(application.kinds()).toEqual(["loading"]);
    expect(application.latestPresentation().reset).toBe("hidden");

    pending.resolve(snapshot);
    await startup;

    expect(application.kinds()).toEqual(["loading", "ready"]);
    expect(application.latestPresentation()).toMatchObject({
      heading: "Local snapshot ready",
      reset: "enabled",
    });
    expect(application.latestState()).toMatchObject({
      kind: "ready",
      snapshot,
    });
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it.each([
    ["empty", async () => undefined, "empty"],
    [
      "unavailable",
      async () => {
        throw new SnapshotStorageError("unavailable", new Error("private"));
      },
      "storage-error",
    ],
    [
      "corrupt",
      async () => {
        throw new SnapshotStorageError("corrupt", new Error("private"));
      },
      "storage-error",
    ],
  ])("renders blocking %s startup without fetch", async (_, load, expected) => {
    const application = applicationDouble(load);
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);

    await startApplication(application.dependencies);

    expect(application.kinds()).toEqual(["loading", expected]);
    expect(application.latestPresentation().reset).toBe("enabled");
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});

describe("application reset composition", () => {
  it("uses the exact confirmation and makes cancellation a complete no-op", async () => {
    const application = applicationDouble(async () => snapshot);
    application.confirm.mockReturnValue(false);
    await startApplication(application.dependencies);
    const before = [...application.renders];

    await application.latestReset()();

    expect(application.confirm).toHaveBeenCalledWith(resetConfirmationText);
    expect(application.reset).not.toHaveBeenCalled();
    expect(application.renders).toEqual(before);
  });

  it("disables reset while deleting and while a blocked request is pending", async () => {
    const pending = deferred<void>();
    let report: ((progress: LocalResetProgress) => void) | undefined;
    const application = applicationDouble(async () => snapshot);
    application.confirm.mockReturnValue(true);
    application.reset.mockImplementation(async (notify) => {
      report = notify;
      return pending.promise;
    });
    await startApplication(application.dependencies);

    const reset = application.latestReset()();
    expect(application.latestState()).toEqual({
      kind: "resetting",
      blocked: false,
    });
    expect(application.latestPresentation().reset).toBe("disabled");

    report?.("database-blocked");
    expect(application.latestState()).toEqual({
      kind: "resetting",
      blocked: true,
    });
    expect(application.latestPresentation()).toMatchObject({
      heading: "Waiting for local storage",
      reset: "disabled",
    });

    pending.resolve();
    await reset;
  });

  it("shows a safe retryable error and reconfirms on retry", async () => {
    const cause = new Error("secret browser detail");
    const application = applicationDouble(async () => undefined);
    application.confirm.mockReturnValue(true);
    application.reset
      .mockRejectedValueOnce(new LocalResetError("cache", cause))
      .mockResolvedValueOnce(undefined);
    await startApplication(application.dependencies);

    await application.latestReset()();

    expect(application.latestState()).toMatchObject({
      kind: "reset-error",
      error: { stage: "cache", cause },
    });
    expect(application.latestPresentation()).toMatchObject({
      heading: "Local reset incomplete",
      reset: "enabled",
      resetLabel: "Retry reset",
    });
    expect(application.latestPresentation().message).not.toContain(
      "secret browser detail",
    );

    await application.latestReset()();

    expect(application.confirm).toHaveBeenCalledTimes(2);
    expect(application.reset).toHaveBeenCalledTimes(2);
  });
});

describe("due synchronization composition", () => {
  it("retains the old snapshot and coalesces onto the first signal and exact promise", async () => {
    const pending = deferred<SnapshotV1>();
    const application = applicationDouble(async () => snapshot);
    application.synchronize.mockReturnValueOnce(pending.promise);
    const controller = await startApplication(application.dependencies);
    const owner = new AbortController();
    const ignored = new AbortController();

    const first = controller.synchronizeWhenDue(owner.signal);
    const second = controller.synchronizeWhenDue(ignored.signal);

    expect(second).toBe(first);
    expect(application.synchronize).toHaveBeenCalledOnce();
    expect(application.synchronize).toHaveBeenCalledWith(owner.signal);
    expect(application.latestState()).toEqual({
      kind: "synchronizing",
      snapshot,
    });
    expect(application.latestPresentation().reset).toBe("disabled");

    ignored.abort(new Error("ignored"));
    expect(application.latestState().kind).toBe("synchronizing");
    await application.latestReset()();
    expect(application.confirm).not.toHaveBeenCalled();
    expect(application.reset).not.toHaveBeenCalled();

    pending.resolve(replacementSnapshot);
    await first;
    expect(application.latestState()).toEqual({
      kind: "ready",
      snapshot: replacementSnapshot,
    });
  });

  it("clears the latch after failure so a later scheduled call can succeed", async () => {
    const application = applicationDouble(async () => snapshot);
    application.synchronize
      .mockRejectedValueOnce(
        new SnapshotSynchronizationError("network", new Error("private")),
      )
      .mockResolvedValueOnce(replacementSnapshot);
    const controller = await startApplication(application.dependencies);

    const failed = controller.synchronizeWhenDue(new AbortController().signal);
    await failed;
    expect(application.latestState()).toMatchObject({
      kind: "synchronization-error",
      snapshot,
      error: { kind: "network" },
    });

    const retried = controller.synchronizeWhenDue(new AbortController().signal);
    expect(retried).not.toBe(failed);
    expect(application.latestState()).toMatchObject({
      kind: "synchronizing",
      snapshot,
    });
    await retried;

    expect(application.synchronize).toHaveBeenCalledTimes(2);
    expect(application.latestState()).toEqual({
      kind: "ready",
      snapshot: replacementSnapshot,
    });
  });

  it("classifies the owning abort only in the UI and keeps first sync empty", async () => {
    const application = applicationDouble(async () => undefined);
    application.synchronize.mockImplementationOnce(
      (signal: AbortSignal) =>
        new Promise<SnapshotV1>((_, reject) =>
          signal.addEventListener("abort", () => reject(signal.reason), {
            once: true,
          }),
        ),
    );
    const controller = await startApplication(application.dependencies);
    const owner = new AbortController();
    const reason = new Error("secret cancellation detail");

    const synchronization = controller.synchronizeWhenDue(owner.signal);
    owner.abort(reason);
    await synchronization;

    expect(application.latestState()).toMatchObject({
      kind: "synchronization-error",
      error: { kind: "canceled", cause: reason },
    });
    expect(application.latestState()).toHaveProperty("snapshot", undefined);
    expect(application.latestPresentation().message).not.toContain(
      reason.message,
    );
    expect(application.latestPresentation().reset).toBe("enabled");
  });
});

describe("pure application presentation", () => {
  it.each([
    [{ kind: "loading" }, "hidden", "Opening local storage"],
    [{ kind: "ready", snapshot }, "enabled", "Local snapshot ready"],
    [{ kind: "empty" }, "enabled", "No local snapshot"],
    [
      {
        kind: "storage-error",
        error: new SnapshotStorageError("unavailable"),
      },
      "enabled",
      "Local storage is unavailable",
    ],
    [
      { kind: "storage-error", error: new SnapshotStorageError("corrupt") },
      "enabled",
      "Local storage is corrupt",
    ],
    [{ kind: "resetting", blocked: false }, "disabled", "Resetting local data"],
    [
      { kind: "resetting", blocked: true },
      "disabled",
      "Waiting for local storage",
    ],
    [
      {
        kind: "reset-error",
        error: new LocalResetError("database", new Error("private")),
      },
      "enabled",
      "Local reset incomplete",
    ],
  ] as const)(
    "maps %# to exact reset and heading state",
    (state, reset, heading) => {
      expect(applicationPresentation(state as ApplicationState)).toMatchObject({
        heading,
        reset,
      });
    },
  );

  it.each([
    "network",
    "gone",
    "http",
    "invalid-json",
    "invalid-contract",
    "unsupported-version",
    "quota",
    "storage",
    "activation",
  ] satisfies readonly SnapshotSynchronizationErrorKind[])(
    "maps %s synchronization errors to safe retry-later copy",
    (kind) => {
      const presentation = applicationPresentation({
        kind: "synchronization-error",
        snapshot,
        error: new SnapshotSynchronizationError(
          kind,
          new Error("secret detail"),
        ),
      });

      expect(presentation).toMatchObject({
        heading: "Snapshot synchronization failed",
        reset: "enabled",
        role: "alert",
      });
      expect(presentation.message).toContain(
        "The previous complete snapshot remains available.",
      );
      expect(presentation.message).not.toContain("secret detail");
    },
  );
});

function applicationDouble(load: () => Promise<SnapshotV1 | undefined>) {
  const renders: Array<{
    state: ApplicationState;
    reset: () => Promise<void>;
  }> = [];
  const confirm = vi.fn<(message: string) => boolean>(() => true);
  const reset = vi.fn<
    (reportProgress: (progress: LocalResetProgress) => void) => Promise<void>
  >(async () => {});
  const synchronize = vi.fn<(signal: AbortSignal) => Promise<SnapshotV1>>(
    async () => snapshot,
  );
  const dependencies: ApplicationDependencies = {
    confirm,
    load,
    reset,
    synchronize,
    render(state, requestReset) {
      renders.push({ state, reset: requestReset });
    },
  };

  return {
    confirm,
    dependencies,
    renders,
    reset,
    synchronize,
    kinds: () => renders.map(({ state }) => state.kind),
    latestPresentation: () => applicationPresentation(latest(renders).state),
    latestReset: () => latest(renders).reset,
    latestState: () => latest(renders).state,
  };
}

function latest<T>(values: readonly T[]): T {
  const value = values[values.length - 1];
  if (value === undefined) {
    throw new Error("expected a rendered application state");
  }
  return value;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}

const snapshot: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
  observedAt: "2026-07-26T12:00:00Z",
  boards: { state: "failed" },
};

const replacementSnapshot: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y8Y",
  observedAt: "2026-07-25T12:00:00Z",
  boards: { state: "failed" },
};
