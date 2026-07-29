// @vitest-environment jsdom

// This module proves the complete hostile-markup policy and its sole Preact sink.

import DOMPurify from "dompurify";
import { type ComponentChild, render } from "preact";
import { act } from "preact/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import { canonicalPostURL } from "./fourchan-url";
import { PostMarkup, sanitizePostMarkup } from "./post-markup";

const context = { board: "g", thread: 42 } as const;
const mountedRoots: Element[] = [];

afterEach(() => {
  for (const root of mountedRoots.splice(0)) {
    act(() => render(null, root));
    root.remove();
  }
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("supported post markup", () => {
  it.each([
    ["bold", "<b>bold</b>", "b"],
    ["strong", "<strong>strong</strong>", "strong"],
    ["italic", "<i>italic</i>", "i"],
    ["emphasis", "<em>emphasis</em>", "em"],
    ["underline", "<u>underline</u>", "u"],
    ["spoiler", "<s>spoiler</s>", "s"],
    ["inline code", "<code>const value = 1</code>", "code"],
  ] as const)("retains %s", (_, input, selector) => {
    const output = sanitized(input);
    expect(output.querySelector(selector)?.textContent).toBe(
      output.textContent,
    );
  });

  it("retains browser line, word-break, quote, dead-link, and code semantics", () => {
    const output = sanitized(
      '<span class="quote evil" style="color:red">green</span><br>' +
        '<span class="deadlink">dead</span><wbr>' +
        '<pre class="prettyprint attacker">code</pre>',
    );

    expect(output.querySelectorAll("br, wbr")).toHaveLength(2);
    expect(output.querySelector("span")?.className).toBe("text-emerald-400");
    expect(output.querySelectorAll("span")[1]?.className).toBe(
      "text-slate-500 line-through",
    );
    expect(output.querySelector("pre")?.className).toBe(
      "my-2 overflow-x-auto rounded bg-slate-950 p-2 font-mono text-sm",
    );
    expect(output.innerHTML).not.toContain("evil");
    expect(output.innerHTML).not.toContain("attacker");
    expect(output.innerHTML).not.toContain("style");
  });
});

describe("unsupported and malformed markup", () => {
  it.each([
    [
      "nested inert elements",
      "<section>outer <marquee>middle <b>bold</b></marquee> tail</section>",
      "outer middle bold tail",
      "section, marquee",
    ],
    [
      "unknown custom elements",
      "<unknown-one>first <unknown-two>second</unknown-two></unknown-one>",
      "first second",
      "unknown-one, unknown-two",
    ],
  ] as const)(
    "unwraps %s while retaining visible descendants",
    (_, input, text, removed) => {
      const output = sanitized(input);
      expect(output.textContent?.replace(/\s+/g, " ").trim()).toBe(text);
      expect(output.querySelector(removed)).toBeNull();
    },
  );

  it.each([
    ["script", "<script>ACTIVE</script>"],
    ["style", "<style>ACTIVE</style>"],
    ["template", "<template>ACTIVE</template>"],
    ["noscript", "<noscript>ACTIVE</noscript>"],
    ["iframe", '<iframe srcdoc="ACTIVE">ACTIVE</iframe>'],
    ["form", "<form>ACTIVE CONTROL</form>"],
    ["object", "<object>ACTIVE CONTROL</object>"],
    ["SVG", "<svg><text>ACTIVE</text></svg>"],
    ["MathML", "<math><mtext>ACTIVE</mtext></math>"],
  ] as const)("drops %s active content entirely", (_, input) => {
    const output = sanitized(`before${input}after`);
    expect(output.textContent).toBe("beforeafter");
    expect(output.textContent).not.toContain("ACTIVE");
    expect(output.textContent).not.toContain("CONTROL");
  });

  it("lets the detached browser parser repair malformed HTML before rendering", () => {
    const output = sanitized(
      '<b>bold<i>italic</b>tail<a href="https://one.example">one' +
        '<a href="https://two.example">two',
    );

    expect(output.textContent).toBe("bolditalictailonetwo");
    expect(output.querySelectorAll("a")).toHaveLength(2);
    expect(output.querySelector("a a")).toBeNull();
  });
});

describe("attribute and protocol policy", () => {
  it("removes every untrusted attribute and emits only fixed classes and validated hrefs", () => {
    const output = sanitized(
      '<b class="attacker" href="https://attacker.example" id="x" style="color:red" onclick="alert(1)" data-x="x" aria-label="x">safe</b>' +
        '<a class="evil" href="https://example.com/path" target="_blank" rel="opener">link</a>',
    );
    const bold = output.querySelector("b");
    const link = output.querySelector("a");

    expect(bold?.getAttributeNames()).toEqual([]);
    expect(link?.getAttributeNames().sort()).toEqual(["class", "href"]);
    expect(link?.href).toBe("https://example.com/path");
    expect(output.innerHTML).not.toMatch(
      /attacker|onclick|style|data-x|aria-label|target|evil/,
    );
  });

  it.each([
    ["https://example.com/path?q=1#part", "https://example.com/path?q=1#part"],
    ["http://example.com/path", "http://example.com/path"],
    ["//example.com/path", "https://example.com/path"],
    ["/g/catalog", "https://boards.4chan.org/g/catalog"],
    ["#section", "https://boards.4chan.org/g/thread/42#section"],
  ] as const)("keeps safe destination %s", (href, expected) => {
    expect(sanitizedLink(href)?.href).toBe(expected);
  });

  it.each([
    "javascript:alert(1)",
    "java&#x73;cript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "vbscript:msgbox(1)",
    "file:///etc/passwd",
    "blob:https://example.com/id",
    "mailto:reader@example.com",
    "tel:+123456",
  ])("turns blocked protocol %s into visible non-link text", (href) => {
    const output = sanitized(`<a href="${href}">visible label</a>`);
    expect(output.querySelector("a")).toBeNull();
    expect(output.textContent).toBe("visible label");
  });

  it("turns missing and malformed destinations into visible non-link text", () => {
    const output = sanitized('<a>missing</a><a href="https://[">malformed</a>');
    expect(output.querySelector("a")).toBeNull();
    expect(output.textContent).toBe("missingmalformed");
  });

  it.each(["", "   ", "\n\t"])(
    "turns empty href %j into visible non-link text",
    (href) => {
      const output = sanitized(`<a href="${href}">empty label</a>`);
      expect(output.querySelector("a")).toBeNull();
      expect(output.textContent).toBe("empty label");
    },
  );

  it.each(["../catalog", "/g/catalog", "#section", "//example.com/path"])(
    "rejects relative destination %s without valid post context",
    (href) => {
      expect(
        sanitizedLink(href, false, { board: "G/../x", thread: 42 }),
      ).toBeNull();
    },
  );

  it("keeps independently valid absolute HTTP(S) links without post context", () => {
    expect(
      sanitizedLink("https://example.com/path", false, {
        board: "G/../x",
        thread: 42,
      })?.href,
    ).toBe("https://example.com/path");
  });
});

describe("canonical quote destinations", () => {
  it.each([
    ["#p43", "https://boards.4chan.org/g/thread/42#p43"],
    ["/a/thread/99#p100", "https://boards.4chan.org/a/thread/99#p100"],
    [
      "https://boards.4channel.org/a/thread/99/safe-slug#p100",
      "https://boards.4chan.org/a/thread/99#p100",
    ],
    [
      "http://boards.4chan.org/a/res/99#p100",
      "https://boards.4chan.org/a/thread/99#p100",
    ],
    [
      "https://boards.4chan.org/a/thread/99/slug?untrusted=1#other",
      "https://boards.4chan.org/a/thread/99",
    ],
    ["//boards.4chan.org/a/thread/99", "https://boards.4chan.org/a/thread/99"],
  ] as const)("canonicalizes quote %s", (href, expected) => {
    expect(sanitizedLink(href, true)?.href).toBe(expected);
  });

  it("does not mistake a deceptive safe external destination for canonical 4chan", () => {
    expect(
      sanitizedLink("https://boards.4chan.org.example/a/thread/99#p100", true)
        ?.href,
    ).toBe("https://boards.4chan.org.example/a/thread/99#p100");
  });

  it.each([
    "https://boards.4chan.org:8443/a/thread/99#p100",
    "https://reader:secret@boards.4chan.org/a/thread/99#p100",
  ])("keeps noncanonical safe quote-shaped URL %s as external", (href) => {
    expect(sanitizedLink(href, true)?.href).toBe(href);
  });

  it("rejects invalid quote context and invalid canonical coordinates", () => {
    expect(
      sanitizedLink("#p43", true, { board: "G/../x", thread: 42 }),
    ).toBeNull();
    expect(canonicalPostURL("g", 42, 43)).toBe(
      "https://boards.4chan.org/g/thread/42#p43",
    );
    for (const [board, thread, post] of [
      ["G", 42, 43],
      ["g/../x", 42, 43],
      ["g", "42", 43],
      ["g", 42, 0],
      ["g", 42, Number.MAX_SAFE_INTEGER + 1],
    ] as const) {
      expect(canonicalPostURL(board, thread, post)).toBeUndefined();
    }
  });
});

describe("Preact integration boundary", () => {
  it("mounts external links without making a network request", () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);

    const root = mount(
      <PostMarkup
        html='<a href="https://example.com/path">external</a><img src="https://example.com/image.png">'
        board="g"
        thread={42}
      />,
    );

    expect(root.querySelector("a")?.href).toBe("https://example.com/path");
    expect(root.querySelector("img")).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("mounts only the sanitizer result in the main document", () => {
    const fragment = document.createDocumentFragment();
    const marker = document.createElement("strong");
    marker.textContent = "sanitizer output";
    fragment.append(marker);
    vi.spyOn(DOMPurify, "sanitize").mockReturnValueOnce(fragment as never);

    const root = mount(
      <PostMarkup html="raw input must not render" board="g" thread={42} />,
    );

    expect(root.textContent).toBe("sanitizer output");
    expect(root.textContent).not.toContain("raw input must not render");
    expect(root.querySelector("strong")?.textContent).toBe("sanitizer output");
  });

  it("passes sanitizer exceptions to Preact only as ordinary visible text", () => {
    vi.spyOn(DOMPurify, "sanitize").mockImplementationOnce(() => {
      throw new Error("sanitizer unavailable");
    });
    const raw = '<img src=x onerror="alert(1)">literal fallback';

    const root = mount(<PostMarkup html={raw} board="g" thread={42} />);

    expect(root.querySelector("img")).toBeNull();
    expect(root.textContent).toBe(raw);
    expect(root.innerHTML).toContain("&lt;img");
  });
});

function sanitized(
  input: string,
  selectedContext: {
    readonly board: string;
    readonly thread: number;
  } = context,
): HTMLDivElement {
  const output = document.createElement("div");
  output.innerHTML = sanitizePostMarkup(input, selectedContext);
  return output;
}

function sanitizedLink(
  href: string,
  quote = false,
  selectedContext: {
    readonly board: string;
    readonly thread: number;
  } = context,
): HTMLAnchorElement | null {
  const className = quote ? ' class="quotelink"' : "";
  return sanitized(
    `<a${className} href="${href}">visible label</a>`,
    selectedContext,
  ).querySelector("a");
}

function mount(component: ComponentChild): HTMLDivElement {
  const root = document.createElement("div");
  document.body.append(root);
  mountedRoots.push(root);
  act(() => render(component, root));
  return root;
}
