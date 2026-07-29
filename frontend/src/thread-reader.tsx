// This module renders one selected local thread in stored order with visual-only reply depth.

import { canonicalPostURL, canonicalThreadURL } from "./fourchan-url";
import { PostMarkup, postQuoteNumbers } from "./post-markup";
import type { OpaqueObject, ThreadEntry } from "./snapshot";

const maximumReplyDepth = 6;
const replyIndentClasses = [
  "ms-0",
  "ms-2",
  "ms-4",
  "ms-6",
  "ms-8",
  "ms-10",
  "ms-12",
] as const;

type ThreadContext = {
  readonly board: string;
  readonly thread: number;
};

// ThreadReader renders every stored post and the exact resource degradation state.
export function ThreadReader({
  board,
  entry,
}: {
  readonly board: OpaqueObject;
  readonly entry: ThreadEntry;
}) {
  const threadURL = canonicalThreadURL(board, entry.summary);
  if (entry.thread === undefined) {
    return (
      <ThreadStatus heading="Thread unavailable" href={threadURL}>
        Not available in this snapshot.
      </ThreadStatus>
    );
  }
  if (entry.thread.state === "failed") {
    return (
      <ThreadStatus degraded heading="Thread failed" href={threadURL}>
        This thread failed during snapshot construction.
      </ThreadStatus>
    );
  }

  const posts = entry.thread.posts;
  const context = threadContext(board, entry.summary);
  const depths = deriveReplyDepths(posts, context);

  return (
    <section
      aria-labelledby="selected-thread-heading"
      class="mt-4 min-w-0 rounded-lg border border-slate-700 bg-slate-950 p-3 sm:p-4"
    >
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 class="font-semibold" id="selected-thread-heading">
            {threadLabel(entry.summary)}
          </h4>
          <p class="mt-1 text-sm text-slate-300">
            {posts.length} stored {posts.length === 1 ? "post" : "posts"}
          </p>
        </div>
        <CanonicalLink href={threadURL} label="Open thread on 4chan" />
      </header>

      {entry.thread.state === "oversize" && (
        <p
          class="mt-3 rounded-lg border border-amber-700 bg-amber-950/40 p-3 text-sm text-amber-100"
          role="status"
        >
          Showing the first 250 posts stored in this snapshot. This thread was
          truncated; later posts are not available.
        </p>
      )}

      {posts.length === 0 ? (
        <p class="mt-4 text-sm text-slate-300" role="status">
          No posts were stored for this thread in this snapshot.
        </p>
      ) : (
        <ol aria-label="Stored posts" class="mt-4 grid gap-3">
          {posts.map((post, index) => (
            <Post
              board={board}
              context={context}
              depth={depths[index] ?? 0}
              index={index}
              key={index}
              post={post}
              summary={entry.summary}
            />
          ))}
        </ol>
      )}
    </section>
  );
}

// deriveReplyDepths returns one visual depth for each unchanged array position.
export function deriveReplyDepths(
  posts: readonly OpaqueObject[],
  context: ThreadContext,
): readonly number[] {
  const numbers = posts.map((post) => positiveInteger(post.no));
  const counts = new Map<number, number>();
  for (const number of numbers) {
    if (number !== undefined) {
      counts.set(number, (counts.get(number) ?? 0) + 1);
    }
  }

  const positions = new Map<number, number>();
  numbers.forEach((number, index) => {
    if (number !== undefined && counts.get(number) === 1) {
      positions.set(number, index);
    }
  });

  const depths: number[] = [];
  posts.forEach((post, index) => {
    const body = typeof post.com === "string" ? post.com : undefined;
    const quotes = body === undefined ? [] : postQuoteNumbers(body, context);
    const resto = positiveInteger(post.resto);
    const relationships =
      resto === context.thread ? [...quotes, resto] : quotes;
    const parent = relationships.find((number) => {
      if (number === undefined) {
        return false;
      }
      const position = positions.get(number);
      return position !== undefined && position < index;
    });
    const parentIndex =
      parent === undefined ? undefined : positions.get(parent);
    depths.push(
      parentIndex === undefined
        ? 0
        : Math.min((depths[parentIndex] ?? 0) + 1, maximumReplyDepth),
    );
  });

  return depths;
}

function Post({
  board,
  summary,
  post,
  index,
  depth,
  context,
}: {
  readonly board: OpaqueObject;
  readonly summary: OpaqueObject;
  readonly post: OpaqueObject;
  readonly index: number;
  readonly depth: number;
  readonly context: ThreadContext;
}) {
  const body = typeof post.com === "string" ? post.com : undefined;
  const details = postDetails(post);
  const postNumber = positiveInteger(post.no);
  const href =
    postNumber === undefined
      ? undefined
      : canonicalPostURL(board.board, summary.no, postNumber);

  return (
    <li class={replyIndentClasses[depth] ?? replyIndentClasses[0]}>
      <details class="rounded-lg border border-slate-700 bg-slate-900" open>
        <summary class="flex min-h-11 cursor-pointer items-center justify-between gap-3 rounded-lg px-3 py-2 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400">
          <span>{postLabel(post, index)}</span>
          {details !== undefined && (
            <span class="min-w-0 truncate text-xs font-normal text-slate-400">
              {details}
            </span>
          )}
        </summary>
        <div class="border-t border-slate-800 p-3">
          {body === undefined ? (
            <p class="text-sm text-slate-300">Post body unavailable.</p>
          ) : (
            <PostMarkup
              board={context.board}
              html={body}
              thread={context.thread}
            />
          )}
          <CanonicalLink href={href} label="Open post on 4chan" />
        </div>
      </details>
    </li>
  );
}

function ThreadStatus({
  heading,
  href,
  children,
  degraded = false,
}: {
  readonly heading: string;
  readonly href?: string;
  readonly children: string;
  readonly degraded?: boolean;
}) {
  return (
    <section
      class={`mt-4 rounded-lg border p-4 ${
        degraded
          ? "border-amber-700 bg-amber-950/40 text-amber-100"
          : "border-slate-700 bg-slate-950 text-slate-200"
      }`}
      role="status"
    >
      <div class="flex items-start justify-between gap-3">
        <div>
          <h4 class="font-semibold">{heading}</h4>
          <p class="mt-1 text-sm">{children}</p>
        </div>
        <CanonicalLink href={href} label="Open thread on 4chan" />
      </div>
    </section>
  );
}

function CanonicalLink({
  href,
  label,
}: {
  readonly href?: string;
  readonly label: string;
}) {
  return href === undefined ? null : (
    <a
      aria-label={label}
      class="mt-2 inline-block rounded-md px-2 py-2 text-xs font-semibold text-cyan-300 underline decoration-cyan-700 underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400"
      href={href}
    >
      4chan ↗
    </a>
  );
}

function threadContext(
  board: OpaqueObject,
  summary: OpaqueObject,
): ThreadContext {
  return {
    board: typeof board.board === "string" ? board.board : "",
    thread: positiveInteger(summary.no) ?? 0,
  };
}

function threadLabel(summary: OpaqueObject): string {
  const number = positiveInteger(summary.no);
  const subject = textField(summary, "sub") ?? textField(summary, "subject");
  if (number !== undefined && subject !== undefined) {
    return `Thread No. ${number} · ${subject}`;
  }
  if (number !== undefined) {
    return `Thread No. ${number}`;
  }
  return subject ?? "Selected thread";
}

function postLabel(post: OpaqueObject, index: number): string {
  const number = positiveInteger(post.no);
  return number === undefined ? `Post ${index + 1}` : `No. ${number}`;
}

function postDetails(post: OpaqueObject): string | undefined {
  return (
    [textField(post, "name"), textField(post, "now")]
      .filter((value): value is string => value !== undefined)
      .join(" · ") || undefined
  );
}

function textField(object: OpaqueObject, field: string): string | undefined {
  const value = object[field];
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function positiveInteger(value: unknown): number | undefined {
  return Number.isSafeInteger(value) && Number(value) > 0
    ? Number(value)
    : undefined;
}
