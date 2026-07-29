// This module proves ordered local browsing, safe links, age refresh, and native controls.

import { type ComponentChildren, Fragment, render, type VNode } from "preact";
import { act } from "preact/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "./app";
import {
  BoardCatalogView,
  type CatalogSelection,
  canonicalBoardURL,
  canonicalThreadURL,
  formatSnapshotAge,
  startSnapshotAgeClock,
} from "./board-catalog";
import type { SnapshotV1 } from "./snapshot";
import { SnapshotSynchronizationError } from "./snapshot-sync";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("ordered local catalog browsing", () => {
  it("mounts component state across clicks and same-snapshot application rerenders", async () => {
    vi.useFakeTimers();
    const host = mountedHost();
    const localSnapshot = integrationSnapshot();
    const reset = vi.fn(async () => {});

    await act(() =>
      render(
        <App
          state={{ kind: "ready", snapshot: localSnapshot }}
          onReset={reset}
        />,
        host.root,
      ),
    );
    expect(vi.getTimerCount()).toBe(1);

    await host.click("/a/ · Second board");
    expect(host.text()).toContain("Page 9");
    expect(host.text()).not.toContain("Failed first");

    await host.click("/z/ · Last upstream");
    await host.click("Page 1");
    expect(host.text()).toContain("Second page absent");
    expect(host.text()).not.toContain("Failed first");
    await host.click("No. 7 · Second page absent");
    expect(host.text()).toContain(
      "Thread unavailableNot available in this snapshot.",
    );

    await act(() =>
      render(
        <App
          state={{ kind: "synchronizing", snapshot: localSnapshot }}
          onReset={reset}
        />,
        host.root,
      ),
    );
    expect(host.text()).toContain(
      "Thread unavailableNot available in this snapshot.",
    );
    await act(() =>
      render(
        <App
          state={{
            kind: "synchronization-error",
            snapshot: localSnapshot,
            error: new SnapshotSynchronizationError("network"),
          }}
          onReset={reset}
        />,
        host.root,
      ),
    );
    expect(host.text()).toContain(
      "Thread unavailableNot available in this snapshot.",
    );

    const sameLineageReplacement = { ...localSnapshot };
    expect(sameLineageReplacement.lineageId).toBe(localSnapshot.lineageId);
    await act(() =>
      render(
        <App
          state={{ kind: "ready", snapshot: sameLineageReplacement }}
          onReset={reset}
        />,
        host.root,
      ),
    );
    expect(host.text()).not.toContain("Failed first");
    expect(host.text()).toContain("Thread unavailable");
    expect(
      host.button("/z/ · Last upstream").getAttribute("aria-pressed"),
    ).toBe("true");
    expect(host.button("Page 1").getAttribute("aria-current")).toBe("page");
    expect(vi.getTimerCount()).toBe(1);

    await act(() => render(null, host.root));
    expect(vi.getTimerCount()).toBe(0);
  });

  it("keeps board, page, and thread positions while every selection performs zero fetches", () => {
    const fetchSpy = vi.fn();
    const pushState = vi.fn();
    const replaceState = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    vi.stubGlobal("history", { pushState, replaceState });
    let selection: CatalogSelection = { boardIndex: 0, pageIndex: 0 };
    const renderView = () =>
      BoardCatalogView({
        now: Date.parse(snapshot.observedAt),
        onSelection: (next) => {
          selection = next;
        },
        selection,
        snapshot,
      });

    let view = renderView();
    expect(buttonTexts(view, "Boards")).toEqual([
      "/z/ · Last upstream",
      "/a/ · First alphabeticallyNot available",
      "/f/ · Failed catalogFailed",
    ]);
    expect(buttonTexts(view, "Catalog pages")).toEqual(["Page 2", "Page 1"]);
    expect(threadButtonTexts(view)).toEqual([
      "No. 9 · Failed firstZed · 2 replies · Failed",
      "No. 8 · Present secondSummary details unavailable",
    ]);
    expect(text(view)).not.toContain("Second page absent");

    clickButton(view, "No. 9 · Failed first");
    view = renderView();
    expect(text(view)).toContain("Thread failed");
    clickButton(view, "No. 8 · Present second");
    view = renderView();
    expect(text(view)).toContain("Thread No. 8");
    expect(text(view)).toContain("1 stored post");

    clickButton(view, "Page 1");
    expect(selection).toEqual({ boardIndex: 0, pageIndex: 1 });
    view = renderView();
    expect(threadButtonTexts(view)).toEqual([
      "No. 7 · Second page absentSummary details unavailable · Not available",
      "No. 6 · Second page truncatedSummary details unavailable · Truncated",
    ]);
    expect(text(view)).not.toContain("Failed first");

    clickButton(view, "No. 7 · Second page absent");
    expect(selection).toEqual({ boardIndex: 0, pageIndex: 1, threadIndex: 0 });
    view = renderView();
    expect(text(view)).toContain(
      "Thread unavailableNot available in this snapshot.",
    );
    clickButton(view, "No. 6 · Second page truncated");
    view = renderView();
    expect(text(view)).toContain(
      "Showing the first 250 posts stored in this snapshot. This thread was truncated; later posts are not available.",
    );

    clickButton(view, "/a/ · First alphabetically");
    expect(selection).toEqual({ boardIndex: 1, pageIndex: 0 });
    view = renderView();
    expect(text(view)).toContain(
      "Catalog unavailableNot available in this snapshot",
    );
    clickButton(view, "/f/ · Failed catalog");
    view = renderView();
    expect(text(view)).toContain("Catalog failed");
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(pushState).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
  });

  it("guards out-of-bounds selection and reports every empty or degraded snapshot state with metadata", () => {
    const cases: readonly [SnapshotV1, string][] = [
      [withBoards({ state: "failed" }), "Board list failed"],
      [withBoards({ state: "present", items: [] }), "No boards"],
      [
        withBoards({ state: "present", items: [{ board: { board: "a" } }] }),
        "Catalog unavailable",
      ],
      [
        withBoards({
          state: "present",
          items: [{ board: { board: "a" }, catalog: { state: "failed" } }],
        }),
        "Catalog failed",
      ],
      [
        withBoards({
          state: "present",
          items: [
            {
              board: { board: "a" },
              catalog: { state: "present", pages: [] },
            },
          ],
        }),
        "Empty catalog",
      ],
      [
        withBoards({
          state: "present",
          items: [
            {
              board: { board: "a" },
              catalog: {
                state: "present",
                pages: [{ metadata: {}, threads: [] }],
              },
            },
          ],
        }),
        "Empty page",
      ],
    ];

    for (const [candidate, expected] of cases) {
      const view = BoardCatalogView({
        now: Date.parse(candidate.observedAt) + 60_000,
        onSelection: () => {},
        selection: { boardIndex: 99, pageIndex: 99, threadIndex: 99 },
        snapshot: candidate,
      });
      expect(text(view)).toContain(candidate.lineageId);
      expect(text(view)).toContain("1 minute ago");
      expect(text(view)).toContain(expected);
    }
  });

  it("renders only narrow textual fields and native responsive controls", () => {
    const view = BoardCatalogView({
      now: Date.parse(snapshot.observedAt),
      onSelection: () => {},
      selection: { boardIndex: 0, pageIndex: 0 },
      snapshot,
    });

    expect(text(view)).not.toContain("opaque-secret");
    expect(text(view)).not.toContain("post body must not render");
    expect(text(view)).not.toContain("stored post body must not render");
    const boardNavigation = elementByAttribute(
      view,
      "nav",
      "aria-label",
      "Boards",
    );
    const pageNavigation = elementByAttribute(
      view,
      "nav",
      "aria-label",
      "Catalog pages",
    );
    expect(text(boardNavigation)).toContain("Boards");
    expect(pageNavigation).toBeDefined();
    expect(
      elements(boardNavigation, "ol").some(({ props }) =>
        String(props.class).includes("sm:grid-cols-2 lg:grid-cols-1"),
      ),
    ).toBe(true);

    const outerGrid = elements(view, "div").find(({ props }) =>
      String(props.class).includes(
        "lg:grid-cols-[minmax(12rem,18rem)_minmax(0,1fr)]",
      ),
    );
    const compactRow = elements(view, "li").find(({ props }) =>
      String(props.class).includes(
        "sm:grid-cols-[minmax(7rem,1fr)_minmax(10rem,2fr)_auto]",
      ),
    );
    expect(outerGrid).toBeDefined();
    expect(compactRow).toBeDefined();

    const buttons = elements(view, "button");
    expect(buttons.every(({ props }) => props.type === "button")).toBe(true);
    expect(
      buttons.every(({ props }) =>
        String(props.class).includes("focus-visible:outline-2"),
      ),
    ).toBe(true);
    expect(buttons.some(({ props }) => props["aria-pressed"] === true)).toBe(
      true,
    );
    expect(buttons.some(({ props }) => props["aria-current"] === "page")).toBe(
      true,
    );
    expect(
      elements(view, "a").every(
        ({ props }) =>
          new URL(String(props.href)).origin === "https://boards.4chan.org",
      ),
    ).toBe(true);
  });

  it("uses honest positional fallbacks without rendering arbitrary opaque content", () => {
    const fallback = withBoards({
      state: "present",
      items: [
        {
          board: { title: 3, href: "javascript:alert(1)" },
          catalog: {
            state: "present",
            pages: [
              {
                metadata: { label: { secret: true } },
                threads: [
                  {
                    summary: {
                      com: "arbitrary catalog body",
                      url: "javascript:alert(1)",
                    },
                  },
                ],
              },
            ],
          },
        },
      ],
    });
    const view = BoardCatalogView({
      now: Date.parse(fallback.observedAt),
      onSelection: () => {},
      selection: { boardIndex: 8, pageIndex: 8 },
      snapshot: fallback,
    });

    expect(text(view)).toContain("Board 1 · details unavailable");
    expect(text(view)).toContain("Page 1");
    expect(text(view)).toContain("Thread 1 · details unavailable");
    expect(text(view)).not.toContain("arbitrary catalog body");
    expect(elements(view, "a")).toEqual([]);
  });
});

describe("snapshot age", () => {
  it.each([
    [-60_000, "Less than a minute old"],
    [0, "Less than a minute old"],
    [59_999, "Less than a minute old"],
    [60_000, "1 minute ago"],
    [59 * 60_000, "59 minutes ago"],
    [60 * 60_000, "1 hour ago"],
    [24 * 60 * 60_000, "1 day ago"],
  ] as const)(
    "formats an elapsed offset of %i milliseconds",
    (offset, expected) => {
      const observed = "2026-07-29T12:00:00Z";
      expect(formatSnapshotAge(observed, Date.parse(observed) + offset)).toBe(
        expected,
      );
    },
  );

  it("refreshes once per minute and cleanup stops the timer", () => {
    vi.useFakeTimers();
    const refresh = vi.fn();
    const stop = startSnapshotAgeClock(refresh);

    vi.advanceTimersByTime(120_000);
    expect(refresh).toHaveBeenCalledTimes(2);
    stop();
    vi.advanceTimersByTime(120_000);
    expect(refresh).toHaveBeenCalledTimes(2);
  });
});

describe("canonical destinations", () => {
  it("constructs only exact canonical HTTPS board and thread destinations", () => {
    expect(canonicalBoardURL({ board: "g" })).toBe(
      "https://boards.4chan.org/g/",
    );
    expect(
      canonicalThreadURL(
        { board: "g" },
        { no: 42, semantic_url: "safe-thread-title" },
      ),
    ).toBe("https://boards.4chan.org/g/thread/42/safe-thread-title");
    expect(
      canonicalThreadURL(
        { board: "g" },
        { no: 42, semantic_url: "../../outside" },
      ),
    ).toBe("https://boards.4chan.org/g/thread/42");
  });

  it.each([
    [{ board: "G" }, { no: 42 }],
    [{ board: "g/../x" }, { no: 42 }],
    [{ board: "g" }, { no: "42" }],
    [{ board: "g" }, { no: Number.MAX_SAFE_INTEGER + 1 }],
    [
      { url: "javascript:alert(1)" },
      { href: "https://boards.4chan.org/g/thread/42" },
    ],
  ] as const)(
    "rejects invalid or arbitrary coordinates %#",
    (board, summary) => {
      expect(canonicalThreadURL(board, summary)).toBeUndefined();
    },
  );
});

function mountedHost() {
  const document = new HostDocument();
  const root = document.createElementNS("http://www.w3.org/1999/xhtml", "div");
  vi.stubGlobal("document", document);

  return {
    root: root as unknown as Element,
    text: () => root.textContent,
    button(label: string) {
      const button = root
        .descendants("button")
        .find((candidate) => candidate.textContent.startsWith(label));
      if (button === undefined) {
        throw new Error(`missing mounted button ${label}`);
      }
      return button;
    },
    async click(label: string) {
      const button = this.button(label);
      await act(() => {
        button.dispatchEvent(new Event("click"));
      });
    },
  };
}

class HostDocument {
  readonly documentElement = new HostElement(
    "html",
    "http://www.w3.org/1999/xhtml",
  );

  createElementNS(namespaceURI: string, localName: string): HostElement {
    return new HostElement(localName, namespaceURI);
  }

  createTextNode(data: string): HostText {
    return new HostText(data);
  }
}

abstract class HostNode {
  parentNode: HostElement | null = null;
  abstract readonly nodeType: number;
  abstract get textContent(): string;

  get nextSibling(): HostNode | null {
    if (this.parentNode === null) {
      return null;
    }
    const index = this.parentNode.childNodes.indexOf(this);
    return this.parentNode.childNodes[index + 1] ?? null;
  }
}

class HostText extends HostNode {
  readonly nodeType = 3;

  constructor(public data: string | number) {
    super();
  }

  get textContent(): string {
    return String(this.data);
  }
}

class HostElement extends HostNode {
  readonly nodeType = 1;
  readonly onclick: unknown = null;
  readonly childNodes: HostNode[] = [];
  readonly attributes: Array<{ readonly name: string; value: string }> = [];
  readonly style = {
    cssText: "",
    setProperty: (_name: string, _value: unknown) => {},
  };
  private readonly listeners = new Map<
    string,
    Set<(event: Event) => unknown>
  >();

  constructor(
    readonly localName: string,
    readonly namespaceURI: string,
  ) {
    super();
  }

  get firstChild(): HostNode | null {
    return this.childNodes[0] ?? null;
  }

  get textContent(): string {
    return this.childNodes.map((child) => child.textContent).join("");
  }

  insertBefore(child: HostNode, reference: HostNode | null): HostNode {
    child.parentNode?.removeChild(child);
    const index =
      reference === null
        ? this.childNodes.length
        : this.childNodes.indexOf(reference);
    if (index < 0) {
      throw new Error("mounted reference node is not a child");
    }
    this.childNodes.splice(index, 0, child);
    child.parentNode = this;
    return child;
  }

  removeChild(child: HostNode): HostNode {
    const index = this.childNodes.indexOf(child);
    if (index < 0) {
      throw new Error("mounted child node is not present");
    }
    this.childNodes.splice(index, 1);
    child.parentNode = null;
    return child;
  }

  setAttribute(name: string, value: unknown): void {
    const current = this.attributes.find(
      (attribute) => attribute.name === name,
    );
    if (current === undefined) {
      this.attributes.push({ name, value: String(value) });
    } else {
      current.value = String(value);
    }
  }

  getAttribute(name: string): string | null {
    return (
      this.attributes.find((attribute) => attribute.name === name)?.value ??
      null
    );
  }

  removeAttribute(name: string): void {
    const index = this.attributes.findIndex(
      (attribute) => attribute.name === name,
    );
    if (index >= 0) {
      this.attributes.splice(index, 1);
    }
  }

  addEventListener(type: string, listener: (event: Event) => unknown): void {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: (event: Event) => unknown): void {
    this.listeners.get(type)?.delete(listener);
  }

  dispatchEvent(event: Event): boolean {
    for (const listener of this.listeners.get(event.type) ?? []) {
      listener.call(this, event);
    }
    return true;
  }

  descendants(localName: string): HostElement[] {
    const matches: HostElement[] = [];
    for (const child of this.childNodes) {
      if (child instanceof HostElement) {
        if (child.localName === localName) {
          matches.push(child);
        }
        matches.push(...child.descendants(localName));
      }
    }
    return matches;
  }
}

function integrationSnapshot(): SnapshotV1 {
  if (snapshot.boards.state !== "present") {
    throw new Error("integration fixture boards must be present");
  }
  return {
    ...snapshot,
    boards: {
      state: "present",
      items: [
        snapshot.boards.items[0] as (typeof snapshot.boards.items)[number],
        {
          board: { board: "a", title: "Second board" },
          catalog: {
            state: "present",
            pages: [
              {
                metadata: { page: 9 },
                threads: [
                  {
                    summary: { no: 99, sub: "Other board thread" },
                    thread: { state: "present", posts: [] },
                  },
                ],
              },
            ],
          },
        },
      ],
    },
  };
}

function withBoards(boards: SnapshotV1["boards"]): SnapshotV1 {
  return { ...snapshot, boards };
}

function clickButton(root: ComponentChildren, label: string): void {
  const button = elements(root, "button").find((candidate) =>
    text(candidate).startsWith(label),
  );
  if (button === undefined || typeof button.props.onClick !== "function") {
    throw new Error(`missing button ${label}`);
  }
  button.props.onClick();
}

function buttonTexts(root: ComponentChildren, navigation: string): string[] {
  return elements(
    elementByAttribute(root, "nav", "aria-label", navigation),
    "button",
  ).map(text);
}

function threadButtonTexts(root: ComponentChildren): string[] {
  return elements(root, "button")
    .filter(({ props }) =>
      [true, false].includes(props["aria-pressed"] as boolean),
    )
    .map(text)
    .filter((value) => value.startsWith("No.") || value.startsWith("Thread"));
}

function elementByAttribute(
  root: ComponentChildren,
  type: string,
  attribute: string,
  value: unknown,
): TestVNode {
  const element = elements(root, type).find(
    ({ props }) => props[attribute] === value,
  );
  if (element === undefined) {
    throw new Error(`missing ${type}[${attribute}=${String(value)}]`);
  }
  return element;
}

function elements(root: ComponentChildren, type: string): TestVNode[] {
  const result: TestVNode[] = [];
  visit(root, (node) => {
    if (node.type === type) {
      result.push(node);
    }
  });
  return result;
}

function text(root: ComponentChildren): string {
  if (root === null || root === undefined || typeof root === "boolean") {
    return "";
  }
  if (typeof root === "string" || typeof root === "number") {
    return String(root);
  }
  if (Array.isArray(root)) {
    return root.map(text).join("");
  }
  const node = root as TestVNode;
  if (node.type === Fragment) {
    return text(node.props.children);
  }
  if (typeof node.type === "function") {
    return text(renderFunction(node));
  }
  return text(node.props.children);
}

function visit(
  root: ComponentChildren,
  inspect: (node: TestVNode) => void,
): void {
  if (
    root === null ||
    root === undefined ||
    typeof root === "boolean" ||
    typeof root === "string" ||
    typeof root === "number"
  ) {
    return;
  }
  if (Array.isArray(root)) {
    for (const child of root) {
      visit(child, inspect);
    }
    return;
  }

  const node = root as TestVNode;
  if (node.type === Fragment) {
    visit(node.props.children, inspect);
    return;
  }
  if (typeof node.type === "function") {
    visit(renderFunction(node), inspect);
    return;
  }
  inspect(node);
  visit(node.props.children, inspect);
}

function renderFunction(node: TestVNode): ComponentChildren {
  return (node.type as (props: Record<string, unknown>) => ComponentChildren)(
    node.props,
  );
}

type TestVNode = VNode<Record<string, unknown>>;

const snapshot: SnapshotV1 = {
  schemaVersion: 1,
  lineageId: "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
  observedAt: "2026-07-29T12:00:00Z",
  boards: {
    state: "present",
    items: [
      {
        board: {
          board: "z",
          title: "Last upstream",
          nested: { secret: "opaque-secret" },
        },
        catalog: {
          state: "present",
          pages: [
            {
              metadata: { page: 2 },
              threads: [
                {
                  summary: {
                    no: 9,
                    sub: "Failed first",
                    name: "Zed",
                    replies: 2,
                    com: "post body must not render",
                  },
                  thread: { state: "failed" },
                },
                {
                  summary: { no: 8, sub: "Present second" },
                  thread: {
                    state: "present",
                    posts: [{ com: "stored post body must not render" }],
                  },
                },
              ],
            },
            {
              metadata: { page: 1 },
              threads: [
                { summary: { no: 7, sub: "Second page absent" } },
                {
                  summary: { no: 6, sub: "Second page truncated" },
                  thread: {
                    state: "oversize",
                    posts: Array.from({ length: 250 }, (_, index) => ({
                      no: index + 1,
                    })),
                  },
                },
              ],
            },
          ],
        },
      },
      { board: { board: "a", title: "First alphabetically" } },
      {
        board: { board: "f", title: "Failed catalog" },
        catalog: { state: "failed" },
      },
    ],
  },
};
