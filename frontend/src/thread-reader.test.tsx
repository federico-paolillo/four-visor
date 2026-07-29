// @vitest-environment jsdom

// This module proves ordered thread rendering, native collapse, and honest local degradation.

import { type ComponentChild, render } from "preact";
import { act } from "preact/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "./app";
import type { OpaqueObject, SnapshotV1, ThreadEntry } from "./snapshot";
import { deriveReplyDepths, ThreadReader } from "./thread-reader";

const mountedRoots: Element[] = [];
const board = { board: "g", title: "Technology" };
const context = { board: "g", thread: 100 } as const;

afterEach(() => {
  for (const root of mountedRoots.splice(0)) {
    act(() => render(null, root));
    root.remove();
  }
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("visual reply depth", () => {
  it("uses only unique prior relationships and never changes source positions", () => {
    const posts: readonly OpaqueObject[] = [
      { no: 100, resto: 0 },
      { no: 101, com: quote("#p100") },
      { no: 102, com: quote("#p101") },
      { no: 103, com: quote("#p999") + quote("#p102") },
      { no: 104, com: quote("#p100") + quote("#p103") },
      { no: 105, resto: 100, com: quote("#p107") },
      { no: 106, resto: 100, com: quote("#p106") },
      { no: 107, com: quote("#p106") },
      { no: 200 },
      { no: 200 },
      { no: 201, com: quote("#p200") },
      { no: 300, com: quote("#p301") },
      { no: 301, com: quote("#p300") },
      {
        no: 302,
        com: '<script><a class="quotelink" href="#p301">hidden</a></script>',
      },
      { no: 303, com: quote("/a/thread/100#p301") },
    ];

    expect(deriveReplyDepths(posts, context)).toEqual([
      0, 1, 2, 3, 1, 1, 1, 2, 0, 0, 0, 0, 1, 0, 0,
    ]);
    expect(posts.map(({ no }) => no)).toEqual([
      100, 101, 102, 103, 104, 105, 106, 107, 200, 200, 201, 300, 301, 302, 303,
    ]);
  });

  it("caps a deep chain without dropping a post", () => {
    const posts = Array.from({ length: 9 }, (_, index) => ({
      no: 100 + index,
      com: index === 0 ? "root" : quote(`#p${99 + index}`),
    }));

    expect(deriveReplyDepths(posts, context)).toEqual([
      0, 1, 2, 3, 4, 5, 6, 6, 6,
    ]);
    expect(posts).toHaveLength(9);
  });

  it("recognizes a browser-decoded quote class through the sanitizer", () => {
    const posts = [
      { no: 100, com: "root" },
      {
        no: 101,
        com: '<a class="quote&#108;ink" href="#p100">encoded quote</a>',
      },
    ];

    expect(deriveReplyDepths(posts, context)).toEqual([0, 1]);
  });

  it("uses resto only when it identifies the selected thread OP", () => {
    const posts = [
      { no: 100 },
      { no: 101, resto: 100 },
      { no: 102, resto: 101 },
      { no: 103, resto: 999 },
    ];

    expect(deriveReplyDepths(posts, context)).toEqual([0, 1, 0, 0]);
  });
});

describe("mounted thread reader", () => {
  it("renders stored order through PostMarkup with native independent collapse and canonical links", () => {
    const fetchSpy = vi.fn();
    const pushState = vi.fn();
    const replaceState = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    vi.stubGlobal("history", { pushState, replaceState });
    const root = mount(
      <ThreadReader
        board={board}
        entry={presentEntry([
          {
            no: 100,
            name: "Original",
            now: "07/29/26",
            com: "<b>first</b><script>unsafe</script>",
          },
          { no: 102, resto: 100, com: "third upstream" },
          { name: "Fallback", com: quote("#p100") },
          { no: 103, com: 7, href: "javascript:alert(1)" },
        ])}
      />,
    );

    expect(summaryTexts(root)).toEqual([
      "No. 100Original · 07/29/26",
      "No. 102",
      "Post 3Fallback",
      "No. 103",
    ]);
    expect(
      [...root.querySelectorAll("ol > li")].map(({ className }) => className),
    ).toEqual(["ms-0", "ms-2", "ms-2", "ms-0"]);
    expect(root.querySelector("b")?.textContent).toBe("first");
    expect(root.querySelector("script")).toBeNull();
    expect(root.textContent).not.toContain("unsafe");
    expect(root.textContent).toContain("Post body unavailable.");

    const details = [...root.querySelectorAll("details")];
    const summaries = [...root.querySelectorAll("summary")];
    expect(details).toHaveLength(4);
    expect(details.every(({ open }) => open)).toBe(true);
    expect(
      summaries.every(
        ({ className }) =>
          className.includes("min-h-11") &&
          className.includes("focus-visible:outline-2"),
      ),
    ).toBe(true);

    summaries[0]?.click();
    expect(details[0]?.open).toBe(false);
    expect(details[1]?.open).toBe(true);

    expect(
      [...root.querySelectorAll<HTMLAnchorElement>("a")].map(
        ({ href }) => href,
      ),
    ).toEqual([
      "https://boards.4chan.org/g/thread/100",
      "https://boards.4chan.org/g/thread/100#p100",
      "https://boards.4chan.org/g/thread/100#p102",
      "https://boards.4chan.org/g/thread/100#p100",
      "https://boards.4chan.org/g/thread/100#p103",
    ]);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(pushState).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
  });

  it("keeps failed, exact absent, and empty-present states distinct with applicable links", () => {
    for (const [entry, expected, rejected] of [
      [
        { summary: { no: 100 } },
        "Not available in this snapshot.",
        "failed during snapshot construction",
      ],
      [
        { summary: { no: 100 }, thread: { state: "failed" } },
        "This thread failed during snapshot construction.",
        "Not available in this snapshot.",
      ],
      [
        presentEntry([]),
        "No posts were stored for this thread in this snapshot.",
        "Not available in this snapshot.",
      ],
    ] satisfies readonly [ThreadEntry, string, string][]) {
      const root = mount(<ThreadReader board={board} entry={entry} />);
      expect(root.textContent).toContain(expected);
      expect(root.textContent).not.toContain(rejected);
      expect(root.querySelector("a")?.href).toBe(
        "https://boards.4chan.org/g/thread/100",
      );
    }
  });

  it("renders all 250 stored oversize posts and an explicit truncation notice", () => {
    const posts = Array.from({ length: 250 }, (_, index) => ({
      no: 100 + index,
      resto: index === 0 ? 0 : 100,
    }));
    const root = mount(
      <ThreadReader
        board={board}
        entry={{
          summary: { no: 100 },
          thread: { state: "oversize", posts },
        }}
      />,
    );

    expect(root.textContent).toContain("250 stored posts");
    expect(root.textContent).toContain(
      "Showing the first 250 posts stored in this snapshot. This thread was truncated; later posts are not available.",
    );
    expect(root.querySelectorAll("details")).toHaveLength(250);
    expect(summaryTexts(root)[0]).toBe("No. 100");
    expect(summaryTexts(root)[249]).toBe("No. 349");
    expect(root.textContent).not.toContain("No. 350");
  });
});

describe("selection and collapse lifetime", () => {
  it("preserves one lineage, resets between threads and replacement lineages, and stays local", async () => {
    const fetchSpy = vi.fn();
    const pushState = vi.fn();
    const replaceState = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    vi.stubGlobal("history", { pushState, replaceState });
    const root = mount(
      <App state={{ kind: "ready", snapshot }} onReset={async () => {}} />,
    );

    await click(root, "No. 100 · First");
    expect(root.textContent).toContain(snapshot.lineageId);
    expect(root.textContent).toContain("Less than a minute old");
    const firstDetails = root.querySelector("details");
    firstDetails?.querySelector("summary")?.click();
    expect(firstDetails?.open).toBe(false);

    act(() =>
      render(
        <App
          state={{ kind: "synchronizing", snapshot: { ...snapshot } }}
          onReset={async () => {}}
        />,
        root,
      ),
    );
    expect(root.textContent).toContain("first body");
    expect(root.querySelector("details")?.open).toBe(false);

    await click(root, "No. 101 · Second");
    expect(root.textContent).toContain("second body");
    expect(root.querySelector("details")?.open).toBe(true);
    await click(root, "No. 100 · First");
    expect(root.querySelector("details")?.open).toBe(true);

    act(() =>
      render(
        <App
          state={{
            kind: "ready",
            snapshot: { ...snapshot, lineageId: replacementLineageID },
          }}
          onReset={async () => {}}
        />,
        root,
      ),
    );
    expect(root.textContent).toContain(replacementLineageID);
    expect(root.querySelector("details")).toBeNull();
    await click(root, "No. 100 · First");
    expect(root.querySelector("details")?.open).toBe(true);

    expect(fetchSpy).not.toHaveBeenCalled();
    expect(pushState).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
  });

  it("keeps media state with its thread DOM and resets it at selection and lineage boundaries", async () => {
    vi.stubGlobal("navigator", { onLine: false });
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    const root = mount(
      <App
        state={{ kind: "ready", snapshot: mediaSnapshot }}
        onReset={async () => {}}
      />,
    );

    await click(root, "No. 100 · Media");
    expect(root.textContent).toContain("persistent text");
    expect(root.textContent).toContain("Reveal spoiler media");
    expect(root.querySelector("[src]")).toBeNull();

    await click(root, "Reveal spoiler media");
    expect(root.textContent).toContain("persistent text");
    expect(root.textContent).toContain("Media unavailable or offline.");
    await click(root, "Retry media");
    expect(root.querySelector("img")?.src).toBe(
      "https://i.4cdn.org/g/1721234567890s.jpg",
    );
    await click(root, "Load full image");
    expect([...root.querySelectorAll("img")].map(({ src }) => src)).toContain(
      "https://i.4cdn.org/g/1721234567890.jpg",
    );

    const details = root.querySelector("details");
    details?.querySelector("summary")?.click();
    expect(details?.open).toBe(false);
    expect(root.textContent).not.toContain("Reveal spoiler media");
    expect(root.textContent).toContain("persistent text");

    act(() =>
      render(
        <App
          state={{
            kind: "synchronizing",
            snapshot: { ...mediaSnapshot },
          }}
          onReset={async () => {}}
        />,
        root,
      ),
    );
    expect(root.querySelector("details")?.open).toBe(false);
    expect(root.textContent).not.toContain("Reveal spoiler media");
    expect(root.textContent).toContain("persistent text");

    await click(root, "No. 101 · Other");
    await click(root, "No. 100 · Media");
    expect(root.textContent).toContain("Reveal spoiler media");
    expect(root.querySelector("[src]")).toBeNull();

    act(() =>
      render(
        <App
          state={{
            kind: "ready",
            snapshot: {
              ...mediaSnapshot,
              lineageId: replacementLineageID,
            },
          }}
          onReset={async () => {}}
        />,
        root,
      ),
    );
    expect(root.querySelector("details")).toBeNull();
    await click(root, "No. 100 · Media");
    expect(root.textContent).toContain("Reveal spoiler media");
    expect(root.textContent).toContain("persistent text");
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});

function presentEntry(posts: readonly OpaqueObject[]): ThreadEntry {
  return {
    summary: { no: 100 },
    thread: { state: "present", posts },
  };
}

function quote(href: string): string {
  return `<a class="quotelink" href="${href}">&gt;&gt;quote</a>`;
}

function mount(component: ComponentChild): HTMLDivElement {
  const root = document.createElement("div");
  document.body.append(root);
  mountedRoots.push(root);
  act(() => render(component, root));
  return root;
}

async function click(root: ParentNode, label: string): Promise<void> {
  const button = [...root.querySelectorAll("button")].find((candidate) =>
    candidate.textContent?.startsWith(label),
  );
  if (button === undefined) {
    throw new Error(`missing button ${label}`);
  }
  await act(() => button.click());
}

function summaryTexts(root: ParentNode): string[] {
  return [...root.querySelectorAll("summary")].map(
    ({ textContent }) => textContent ?? "",
  );
}

const snapshot: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
  observedAt: new Date().toISOString(),
  boards: {
    state: "present",
    items: [
      {
        board,
        catalog: {
          state: "present",
          pages: [
            {
              metadata: { page: 1 },
              threads: [
                {
                  summary: { no: 100, sub: "First" },
                  thread: {
                    state: "present",
                    posts: [{ no: 100, com: "first body" }],
                  },
                },
                {
                  summary: { no: 101, sub: "Second" },
                  thread: {
                    state: "present",
                    posts: [{ no: 101, com: "second body" }],
                  },
                },
              ],
            },
          ],
        },
      },
    ],
  },
};

const replacementLineageID = "01J1YQ7Y0M4S6R8T2V3W5X7Y8Y";

const mediaSnapshot: SnapshotV1 = {
  ...snapshot,
  boards: {
    state: "present",
    items: [
      {
        board,
        catalog: {
          state: "present",
          pages: [
            {
              metadata: { page: 1 },
              threads: [
                {
                  summary: { no: 100, sub: "Media" },
                  thread: {
                    state: "present",
                    posts: [
                      {
                        no: 100,
                        com: "<b>persistent text</b>",
                        ext: ".jpg",
                        filename: "example",
                        h: 720,
                        spoiler: 1,
                        tim: 1_721_234_567_890,
                        tn_h: 125,
                        tn_w: 111,
                        w: 1280,
                      },
                    ],
                  },
                },
                {
                  summary: { no: 101, sub: "Other" },
                  thread: {
                    state: "present",
                    posts: [{ no: 101, com: "other body" }],
                  },
                },
              ],
            },
          ],
        },
      },
    ],
  },
};
