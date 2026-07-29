// @vitest-environment jsdom

// This module proves direct native media eligibility, intent gates, and the opaque-field boundary.

import { type ComponentChild, render } from "preact";
import { act } from "preact/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import { canonicalPostMedia } from "./fourchan-url";
import { PostMedia } from "./post-media";
import type { OpaqueObject } from "./snapshot";

const productionComponents = import.meta.glob(["./*.tsx", "!./*.test.tsx"], {
  eager: true,
  import: "default",
  query: "?raw",
}) as Record<string, string>;
const mountedRoots: Element[] = [];
const board = { board: "g" };
const basePost = {
  ext: ".jpg",
  filename: "example",
  h: 720,
  tim: 1_721_234_567_890,
  tn_h: 125,
  tn_w: 111,
  w: 1280,
};
const thumbnailURL = "https://i.4cdn.org/g/1721234567890s.jpg";
const fullImageURL = "https://i.4cdn.org/g/1721234567890.jpg";

afterEach(() => {
  for (const root of mountedRoots.splice(0)) {
    act(() => render(null, root));
    root.remove();
  }
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("canonical attachment boundary", () => {
  it.each([
    ["image", ".jpg", "image"],
    ["video", ".webm", "video"],
    ["audio", ".mp3", "audio"],
    ["file", ".pdf", "file"],
  ] as const)(
    "derives direct %s URLs from validated coordinates",
    (_, ext, kind) => {
      expect(canonicalPostMedia(board, { ...basePost, ext })).toEqual({
        filename: `example${ext}`,
        fullURL: `https://i.4cdn.org/g/1721234567890${ext}`,
        height: 720,
        kind,
        spoiler: false,
        thumbnailHeight: 125,
        thumbnailURL,
        thumbnailWidth: 111,
        width: 1280,
      });
    },
  );

  it("accepts only absent or exact numeric spoiler state", () => {
    expect(
      canonicalPostMedia(board, { ...basePost, spoiler: 1 }),
    ).toMatchObject({
      spoiler: true,
    });
    for (const spoiler of [0, 2, true, "1", null]) {
      expect(
        canonicalPostMedia(board, { ...basePost, spoiler }),
      ).toBeUndefined();
    }
  });

  it.each([
    ["missing board", {}, basePost],
    ["upper-case board", { board: "G" }, basePost],
    ["path board", { board: "g/../x" }, basePost],
    ["non-string board", { board: 3 }, basePost],
    [
      "inherited fields",
      Object.create(board) as OpaqueObject,
      Object.create(basePost) as OpaqueObject,
    ],
    ["missing tim", board, without(basePost, "tim")],
    ["string tim", board, { ...basePost, tim: "1721234567890" }],
    ["zero tim", board, { ...basePost, tim: 0 }],
    ["fractional tim", board, { ...basePost, tim: 1.5 }],
    ["unsafe tim", board, { ...basePost, tim: Number.MAX_SAFE_INTEGER + 1 }],
    ["missing ext", board, without(basePost, "ext")],
    ["non-string ext", board, { ...basePost, ext: 3 }],
    ["blank ext", board, { ...basePost, ext: " " }],
    ["upper-case ext", board, { ...basePost, ext: ".JPG" }],
    ["bare ext", board, { ...basePost, ext: "jpg" }],
    ["path ext", board, { ...basePost, ext: "/x.jpg" }],
    ["query ext", board, { ...basePost, ext: ".jpg?x=1" }],
    ["fragment ext", board, { ...basePost, ext: ".jpg#x" }],
    ["missing filename", board, without(basePost, "filename")],
    ["blank filename", board, { ...basePost, filename: "  " }],
    ["non-string filename", board, { ...basePost, filename: 3 }],
    ["missing width", board, without(basePost, "w")],
    ["zero width", board, { ...basePost, w: 0 }],
    ["missing height", board, without(basePost, "h")],
    ["string height", board, { ...basePost, h: "720" }],
    ["missing thumbnail width", board, without(basePost, "tn_w")],
    ["fractional thumbnail width", board, { ...basePost, tn_w: 1.5 }],
    ["missing thumbnail height", board, without(basePost, "tn_h")],
    [
      "unsafe thumbnail height",
      board,
      { ...basePost, tn_h: Number.MAX_SAFE_INTEGER + 1 },
    ],
  ] as const)(
    "rejects %s without a request or link",
    (_, candidateBoard, post) => {
      expect(canonicalPostMedia(candidateBoard, post)).toBeUndefined();
      const root = mount(<PostMedia board={candidateBoard} post={post} />);
      expect(root.querySelector("[src], a[href]")).toBeNull();
    },
  );

  it("ignores every opaque URL-shaped field", () => {
    const hostile = {
      ...basePost,
      href: "https://attacker.example/file.jpg",
      semantic_url: "https://attacker.example/semantic.jpg",
      url: "javascript:alert(1)",
    };
    expect(canonicalPostMedia(board, hostile)).toMatchObject({
      fullURL: fullImageURL,
      thumbnailURL,
    });
    expect(
      canonicalPostMedia(
        { semantic_url: "g", url: "https://i.4cdn.org/g/1.jpg" },
        { href: fullImageURL, semantic_url: fullImageURL, url: fullImageURL },
      ),
    ).toBeUndefined();
  });

  it("rejects trusted own accessors without invoking their getters", () => {
    for (const field of [
      "board",
      "tim",
      "ext",
      "filename",
      "spoiler",
      "w",
      "h",
      "tn_w",
      "tn_h",
    ]) {
      const candidateBoard = { ...board } as OpaqueObject;
      const candidatePost = { ...basePost } as OpaqueObject;
      const getter = vi.fn(() => "untrusted");
      Object.defineProperty(
        field === "board" ? candidateBoard : candidatePost,
        field,
        {
          enumerable: true,
          get: getter,
        },
      );

      expect(canonicalPostMedia(candidateBoard, candidatePost)).toBeUndefined();
      expect(getter).not.toHaveBeenCalled();
    }
  });

  it("ignores irrelevant own accessors without invoking their getters", () => {
    const candidatePost = { ...basePost } as OpaqueObject;
    const getter = vi.fn(() => "https://attacker.example/media");
    for (const field of ["semantic_url", "url", "href"]) {
      Object.defineProperty(candidatePost, field, {
        enumerable: true,
        get: getter,
      });
    }

    expect(canonicalPostMedia(board, candidatePost)).toMatchObject({
      fullURL: fullImageURL,
      thumbnailURL,
    });
    expect(getter).not.toHaveBeenCalled();
  });

  it("renders an arbitrary filename only as escaped text", () => {
    setOnline(true);
    const filename = '<img src="https://attacker.example/x" onerror="x">';
    const root = mount(
      <PostMedia board={board} post={{ ...basePost, filename }} />,
    );

    expect(root.textContent).toContain(`${filename}.jpg`);
    expect(root.querySelectorAll("img")).toHaveLength(1);
    expect(root.querySelector("[onerror]")).toBeNull();
    expect(sourceURLs(root)).toEqual([thumbnailURL]);
  });
});

describe("native request eligibility", () => {
  it("inserts only the online thumbnail before separate full-image intent", () => {
    setOnline(true);
    const sideEffects = sideEffectSpies();
    const root = mount(<PostMedia board={board} post={basePost} />);

    expect(sourceURLs(root)).toEqual([thumbnailURL]);
    expect(root.innerHTML).not.toContain(fullImageURL);
    const thumbnail = root.querySelector("img");
    expect(thumbnail).toMatchObject({ height: 125, width: 111 });

    click(root, "Load full image");
    expect(sourceURLs(root)).toEqual([thumbnailURL, fullImageURL]);
    const full = root.querySelector<HTMLImageElement>(
      'img[aria-label^="Full image"]',
    );
    expect(full).toMatchObject({ height: 720, width: 1280 });
    sideEffects.expectUnused();
  });

  it("starts offline at the fixed placeholder and retries repeatedly despite stale offline state", () => {
    setOnline(false);
    const sideEffects = sideEffectSpies();
    const root = mount(<PostMedia board={board} post={basePost} />);

    expect(sourceURLs(root)).toEqual([]);
    expect(placeholders(root)).toEqual([
      "▧ Media unavailable or offline.Retry media",
    ]);

    let previous: Element | undefined;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      click(root, "Retry media");
      const thumbnail = root.querySelector("img");
      expect(thumbnail?.getAttribute("src")).toBe(thumbnailURL);
      expect(thumbnail).not.toBe(previous);
      previous = thumbnail ?? undefined;
      act(() => {
        thumbnail?.dispatchEvent(new Event("error"));
      });
      expect(placeholders(root)).toEqual([
        "▧ Media unavailable or offline.Retry media",
      ]);
    }

    expect(navigator.onLine).toBe(false);
    sideEffects.expectUnused();
  });

  it.each([
    ["image", ".jpg", "img"],
    ["video", ".webm", "video"],
    ["audio", ".mp3", "audio"],
  ] as const)(
    "gates full %s insertion and manually remounts its identical URL after error",
    (kind, ext, selector) => {
      setOnline(true);
      const root = mount(
        <PostMedia board={board} post={{ ...basePost, ext }} />,
      );
      const fullURL = `https://i.4cdn.org/g/1721234567890${ext}`;

      expect(root.querySelector(`${selector}[src="${fullURL}"]`)).toBeNull();
      click(root, `Load full ${kind}`);
      const first = root.querySelector(`${selector}[src="${fullURL}"]`);
      expect(first).not.toBeNull();
      if (selector !== "img") {
        expect(first?.getAttribute("preload")).toBe("none");
        expect(first?.hasAttribute("controls")).toBe(true);
        expect(first?.hasAttribute("autoplay")).toBe(false);
        expect(first?.hasAttribute("poster")).toBe(false);
      }
      act(() => {
        first?.dispatchEvent(new Event("error"));
      });
      expect(root.querySelector(`${selector}[src="${fullURL}"]`)).toBeNull();

      click(root, "Retry media");
      const retried = root.querySelector(`${selector}[src="${fullURL}"]`);
      expect(retried).not.toBeNull();
      expect(retried).not.toBe(first);
      expect(retried?.getAttribute("src")).toBe(fullURL);
    },
  );

  it("never lets an offline signal veto explicit full-media intent or retry", () => {
    setOnline(false);
    const root = mount(<PostMedia board={board} post={basePost} />);

    click(root, "Load full image");
    const first = root.querySelector<HTMLImageElement>(
      `img[src="${fullImageURL}"]`,
    );
    expect(first).not.toBeNull();
    act(() => {
      first?.dispatchEvent(new Event("error"));
    });

    const retries = [...root.querySelectorAll("button")].filter(
      ({ textContent }) => textContent === "Retry media",
    );
    expect(retries).toHaveLength(2);
    act(() => retries[1]?.click());
    const retried = root.querySelector<HTMLImageElement>(
      `img[src="${fullImageURL}"]`,
    );
    expect(retried).not.toBeNull();
    expect(retried).not.toBe(first);
    expect(retried?.src).toBe(fullImageURL);
    expect(navigator.onLine).toBe(false);
  });

  it("uses one safe native file link as the repeatable request boundary", () => {
    setOnline(true);
    const sideEffects = sideEffectSpies();
    const root = mount(
      <PostMedia board={board} post={{ ...basePost, ext: ".pdf" }} />,
    );
    const link = root.querySelector("a");
    expect(link).toMatchObject({
      href: "https://i.4cdn.org/g/1721234567890.pdf",
      rel: "noopener noreferrer",
      target: "_blank",
    });
    expect(sourceURLs(root)).toEqual([thumbnailURL]);

    const clicks = vi.fn((event: Event) => event.preventDefault());
    link?.addEventListener("click", clicks);
    act(() => {
      link?.dispatchEvent(new MouseEvent("click", { cancelable: true }));
    });
    act(() => {
      link?.dispatchEvent(new MouseEvent("click", { cancelable: true }));
    });
    expect(clicks).toHaveBeenCalledTimes(2);
    sideEffects.expectUnused();
  });

  it("keeps spoiler metadata and URLs hidden until reveal, then still gates full media", () => {
    setOnline(true);
    const root = mount(
      <PostMedia board={board} post={{ ...basePost, spoiler: 1 }} />,
    );

    expect(root.textContent).toBe("Reveal spoiler media");
    expect(root.innerHTML).not.toContain("example.jpg");
    expect(root.querySelector("[src], a[href]")).toBeNull();

    click(root, "Reveal spoiler media");
    expect(root.textContent).toContain("example.jpg");
    expect(sourceURLs(root)).toEqual([thumbnailURL]);
    expect(root.innerHTML).not.toContain(fullImageURL);

    click(root, "Load full image");
    expect(sourceURLs(root)).toEqual([thumbnailURL, fullImageURL]);
  });

  it("retains PostMarkup as the sole production HTML sink", () => {
    const sinks = Object.entries(productionComponents).flatMap(
      ([path, source]) =>
        [...source.matchAll(/dangerouslySetInnerHTML/g)].map(() => path),
    );
    expect(sinks).toEqual(["./post-markup.tsx"]);
  });
});

function without(object: OpaqueObject, field: string): OpaqueObject {
  return Object.fromEntries(
    Object.entries(object).filter(([candidate]) => candidate !== field),
  );
}

function mount(component: ComponentChild): HTMLDivElement {
  const root = document.createElement("div");
  document.body.append(root);
  mountedRoots.push(root);
  act(() => render(component, root));
  return root;
}

function click(root: ParentNode, label: string): void {
  const button = [...root.querySelectorAll("button")].find(
    (candidate) => candidate.textContent === label,
  );
  if (button === undefined) {
    throw new Error(`missing button ${label}`);
  }
  act(() => button.click());
}

function sourceURLs(root: ParentNode): string[] {
  return [...root.querySelectorAll("[src]")].map(
    (element) => (element as HTMLImageElement).src,
  );
}

function placeholders(root: ParentNode): string[] {
  return [...root.querySelectorAll('[role="status"]')].map(
    ({ textContent }) => textContent?.replace(/\s+/g, " ").trim() ?? "",
  );
}

function setOnline(value: boolean): void {
  vi.stubGlobal("navigator", { onLine: value });
}

function sideEffectSpies() {
  vi.useFakeTimers();
  const timeout = vi.spyOn(globalThis, "setTimeout");
  const interval = vi.spyOn(globalThis, "setInterval");
  const fetchSpy = vi.fn();
  const pushState = vi.fn();
  const replaceState = vi.fn();
  const cacheOpen = vi.fn();
  const cacheDelete = vi.fn();
  const cacheKeys = vi.fn();
  const databaseOpen = vi.fn();
  vi.stubGlobal("fetch", fetchSpy);
  vi.stubGlobal("history", { pushState, replaceState });
  vi.stubGlobal("caches", {
    delete: cacheDelete,
    keys: cacheKeys,
    open: cacheOpen,
  });
  vi.stubGlobal("indexedDB", { open: databaseOpen });

  return {
    expectUnused() {
      expect(fetchSpy).not.toHaveBeenCalled();
      expect(pushState).not.toHaveBeenCalled();
      expect(replaceState).not.toHaveBeenCalled();
      expect(cacheOpen).not.toHaveBeenCalled();
      expect(cacheDelete).not.toHaveBeenCalled();
      expect(cacheKeys).not.toHaveBeenCalled();
      expect(databaseOpen).not.toHaveBeenCalled();
      expect(timeout).not.toHaveBeenCalled();
      expect(interval).not.toHaveBeenCalled();
      expect(vi.getTimerCount()).toBe(0);
    },
  };
}
