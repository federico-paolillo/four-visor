// This module proves the browser validator against the shared snapshot version 1 corpus.

import { describe, expect, it } from "vitest";

import {
  parseSnapshot,
  SnapshotError,
  type SnapshotErrorKind,
  validateSnapshot,
} from "./snapshot";

const fixtures = import.meta.glob("../../testdata/snapshot-v1/**/*.json", {
  eager: true,
  import: "default",
  query: "?raw",
}) as Record<string, string>;

describe("shared snapshot version 1 fixtures", () => {
  const cases: readonly [string, SnapshotErrorKind | undefined][] = [
    ["valid", undefined],
    ["invalid-contract", "invalid-contract"],
    ["unsupported-version", "unsupported-version"],
    ["invalid-json", "invalid-json"],
  ];

  for (const [directory, expectedKind] of cases) {
    const selected = Object.entries(fixtures).filter(([path]) =>
      path.includes(`/snapshot-v1/${directory}/`),
    );

    it(`${directory} corpus is present`, () => {
      expect(selected.length).toBeGreaterThan(0);
    });

    for (const [path, text] of selected) {
      it(path, () => {
        if (expectedKind === undefined) {
          expect(parseSnapshot(text)).toBeDefined();
          return;
        }

        expect(() => parseSnapshot(text)).toThrowError(
          expect.objectContaining({ kind: expectedKind }),
        );
      });
    }
  }
});

it("accepts backend serialization without changing opaque fields or order", () => {
  const text = fixture("/valid/backend-serialized.json");
  const raw: unknown = JSON.parse(text);
  const snapshot = validateSnapshot(raw);

  expect(snapshot).toBe(raw);
  expect(snapshot.observedAt).toBe("2026-07-26T12:00:00+00:00");
  if (snapshot.boards.state !== "present") {
    throw new Error("backend fixture boards must be present");
  }

  expect(snapshot.boards.items.map(({ board }) => board.board)).toEqual(["g"]);
  const catalog = snapshot.boards.items[0]?.catalog;
  if (catalog?.state !== "present") {
    throw new Error("backend fixture catalog must be present");
  }

  expect(catalog.pages.map(({ metadata }) => metadata.page)).toEqual([1]);
  expect(catalog.pages[0]?.threads.map(({ summary }) => summary.no)).toEqual([
    300, 200,
  ]);
  expect(
    Object.keys(catalog.pages[0]?.threads[0]?.summary.custom as object),
  ).toEqual(["z", "a"]);

  const thread = catalog.pages[0]?.threads[0]?.thread;
  if (thread?.state !== "present") {
    throw new Error("backend fixture thread must be present");
  }
  expect(thread.posts.map((post) => post.no)).toEqual([300, 301]);
  expect(thread.posts[0]).toMatchObject({
    com: "<b>first</b>",
    unknown: [3, 1, 2],
  });
});

it("accepts both UTC spellings and preserves ULID casing", () => {
  for (const observedAt of [
    "2026-07-26T12:00:00Z",
    "2026-07-26T12:00:00.12345678901234567890Z",
    "2026-07-26T12:00:00+00:00",
    "2026-07-26T12:00:00.5+00:00",
  ]) {
    const snapshot = parseSnapshot(minimalDocument(observedAt));
    expect(snapshot.lineageId).toBe("01j1yq7y0m4s6r8t2v3w5x7y9z");
    expect(snapshot.observedAt).toBe(observedAt);
  }
});

it("preserves syntax causes and redacts unknown wrapper fields", () => {
  try {
    parseSnapshot('{"schemaVersion":!}');
    throw new Error("parseSnapshot should have failed");
  } catch (error) {
    expect(error).toBeInstanceOf(SnapshotError);
    expect(error).toMatchObject({
      kind: "invalid-json",
      cause: expect.any(SyntaxError),
    });
  }

  const secret = "attacker-controlled-secret";
  const value = JSON.parse(minimalDocument("2026-07-26T12:00:00Z"));
  value[secret] = true;
  try {
    validateSnapshot(value);
    throw new Error("validateSnapshot should have failed");
  } catch (error) {
    expect(error).toBeInstanceOf(SnapshotError);
    expect(String(error)).not.toContain(secret);
  }
});

function fixture(suffix: string): string {
  const entry = Object.entries(fixtures).find(([path]) =>
    path.endsWith(suffix),
  );
  if (entry === undefined) {
    throw new Error(`missing fixture ${suffix}`);
  }

  return entry[1];
}

function minimalDocument(observedAt: string): string {
  return JSON.stringify({
    schemaVersion: 1,
    lineageId: "01j1yq7y0m4s6r8t2v3w5x7y9z",
    observedAt,
    boards: { state: "failed" },
  });
}
