// This module browses one immutable local snapshot without fetching or reshaping it.

import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";

import { canonicalBoardURL, canonicalThreadURL } from "./fourchan-url";
import type {
  BoardItem,
  OpaqueObject,
  Page,
  SnapshotV1,
  ThreadEntry,
} from "./snapshot";
import { ThreadReader } from "./thread-reader";

const ageRefreshInterval = 60_000;
const relativeTime = new Intl.RelativeTimeFormat("en", { numeric: "always" });

export { canonicalBoardURL, canonicalThreadURL } from "./fourchan-url";

export type CatalogSelection = {
  readonly boardIndex: number;
  readonly pageIndex: number;
  readonly threadIndex?: number;
};

const initialSelection: CatalogSelection = {
  boardIndex: 0,
  pageIndex: 0,
};

// BoardCatalog owns transient browsing state for exactly one snapshot instance.
export function BoardCatalog({ snapshot }: { readonly snapshot: SnapshotV1 }) {
  const [selection, setSelection] = useState(initialSelection);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => startSnapshotAgeClock(() => setNow(Date.now())), []);

  return (
    <BoardCatalogView
      now={now}
      onSelection={setSelection}
      selection={selection}
      snapshot={snapshot}
    />
  );
}

// BoardCatalogView renders direct array positions and exposes native selection controls.
export function BoardCatalogView({
  snapshot,
  selection,
  onSelection,
  now,
}: {
  readonly snapshot: SnapshotV1;
  readonly selection: CatalogSelection;
  readonly onSelection: (selection: CatalogSelection) => void;
  readonly now: number;
}) {
  return (
    <section class="mt-6" aria-labelledby="snapshot-heading">
      <header class="rounded-xl border border-slate-800 bg-slate-900 p-4 sm:flex sm:items-start sm:justify-between sm:gap-6">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold" id="snapshot-heading">
            Local snapshot
          </h2>
          <p class="mt-1 break-all font-mono text-xs text-slate-300">
            Lineage {snapshot.lineageId}
          </p>
        </div>
        <p class="mt-2 shrink-0 text-sm text-slate-300 sm:mt-0">
          <time dateTime={snapshot.observedAt}>
            {formatSnapshotAge(snapshot.observedAt, now)}
          </time>
        </p>
      </header>

      <BoardCatalogContent
        onSelection={onSelection}
        selection={selection}
        snapshot={snapshot}
      />
    </section>
  );
}

// formatSnapshotAge converts the validated observation instant into a coarse stable age.
export function formatSnapshotAge(
  observedAt: string,
  now: number = Date.now(),
): string {
  const observed = Date.parse(observedAt);
  if (!Number.isFinite(observed)) {
    return "Age unavailable";
  }

  const minutes = Math.max(0, Math.floor((now - observed) / 60_000));
  if (minutes === 0) {
    return "Less than a minute old";
  }
  if (minutes < 60) {
    return relativeTime.format(-minutes, "minute");
  }

  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return relativeTime.format(-hours, "hour");
  }
  return relativeTime.format(-Math.floor(hours / 24), "day");
}

// startSnapshotAgeClock refreshes age text and returns its exact effect cleanup.
export function startSnapshotAgeClock(refresh: () => void): () => void {
  const interval = setInterval(refresh, ageRefreshInterval);
  return () => clearInterval(interval);
}

function BoardCatalogContent({
  snapshot,
  selection,
  onSelection,
}: {
  readonly snapshot: SnapshotV1;
  readonly selection: CatalogSelection;
  readonly onSelection: (selection: CatalogSelection) => void;
}) {
  if (snapshot.boards.state === "failed") {
    return (
      <ResourceStatus heading="Board list failed" degraded>
        Boards could not be observed for this snapshot.
      </ResourceStatus>
    );
  }
  if (snapshot.boards.items.length === 0) {
    return (
      <ResourceStatus heading="No boards">
        No boards were observed in this snapshot.
      </ResourceStatus>
    );
  }

  const boardIndex = validIndex(selection.boardIndex, snapshot.boards.items)
    ? selection.boardIndex
    : 0;
  const item = snapshot.boards.items[boardIndex];
  if (item === undefined) {
    return null;
  }

  return (
    <div class="mt-4 grid gap-4 lg:grid-cols-[minmax(12rem,18rem)_minmax(0,1fr)]">
      <BoardList
        items={snapshot.boards.items}
        onSelect={(nextBoardIndex) =>
          onSelection({ boardIndex: nextBoardIndex, pageIndex: 0 })
        }
        selectedIndex={boardIndex}
      />
      <CatalogPanel
        item={item}
        itemIndex={boardIndex}
        onSelectPage={(pageIndex) => onSelection({ boardIndex, pageIndex })}
        onSelectThread={(threadIndex) =>
          onSelection({
            boardIndex,
            pageIndex: selectedPageIndex(item, selection.pageIndex),
            threadIndex,
          })
        }
        pageIndex={selectedPageIndex(item, selection.pageIndex)}
        threadIndex={selection.threadIndex}
      />
    </div>
  );
}

function BoardList({
  items,
  selectedIndex,
  onSelect,
}: {
  readonly items: readonly BoardItem[];
  readonly selectedIndex: number;
  readonly onSelect: (index: number) => void;
}) {
  return (
    <nav
      aria-label="Boards"
      class="rounded-xl border border-slate-800 bg-slate-900 p-3"
    >
      <h3 class="px-2 text-sm font-semibold uppercase tracking-wider text-slate-400">
        Boards
      </h3>
      <ol class="mt-2 grid gap-1 sm:grid-cols-2 lg:grid-cols-1">
        {items.map((item, index) => {
          const status = catalogStatus(item);
          return (
            <li
              class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2"
              key={index}
            >
              <button
                aria-pressed={selectedIndex === index}
                class="min-h-11 min-w-0 rounded-lg px-3 py-2 text-left text-sm font-medium text-slate-100 hover:bg-slate-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400 aria-pressed:bg-cyan-500 aria-pressed:text-slate-950"
                onClick={() => onSelect(index)}
                type="button"
              >
                <span class="block truncate">{boardLabel(item, index)}</span>
                {status !== undefined && (
                  <span class="mt-0.5 block text-xs font-semibold">
                    {status}
                  </span>
                )}
              </button>
              <CanonicalLink
                href={canonicalBoardURL(item.board)}
                label={`Open ${boardLabel(item, index)} on 4chan`}
              />
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

function CatalogPanel({
  item,
  itemIndex,
  pageIndex,
  threadIndex,
  onSelectPage,
  onSelectThread,
}: {
  readonly item: BoardItem;
  readonly itemIndex: number;
  readonly pageIndex: number;
  readonly threadIndex?: number;
  readonly onSelectPage: (index: number) => void;
  readonly onSelectThread: (index: number) => void;
}) {
  const catalog = item.catalog;
  return (
    <section class="min-w-0 rounded-xl border border-slate-800 bg-slate-900 p-3 sm:p-4">
      <div class="flex items-start justify-between gap-3">
        <h3 class="min-w-0 truncate text-xl font-semibold">
          {boardLabel(item, itemIndex)}
        </h3>
        <CanonicalLink
          href={canonicalBoardURL(item.board)}
          label={`Open ${boardLabel(item, itemIndex)} on 4chan`}
        />
      </div>

      {catalog === undefined ? (
        <ResourceStatus heading="Catalog unavailable">
          Not available in this snapshot
        </ResourceStatus>
      ) : catalog.state === "failed" ? (
        <ResourceStatus heading="Catalog failed" degraded>
          This catalog failed during snapshot construction.
        </ResourceStatus>
      ) : catalog.pages.length === 0 ? (
        <ResourceStatus heading="Empty catalog">
          No catalog pages were observed in this snapshot.
        </ResourceStatus>
      ) : (
        <PresentCatalog
          board={item.board}
          boardIndex={itemIndex}
          onSelectPage={onSelectPage}
          onSelectThread={onSelectThread}
          pageIndex={pageIndex}
          pages={catalog.pages}
          threadIndex={threadIndex}
        />
      )}
    </section>
  );
}

function PresentCatalog({
  board,
  boardIndex,
  pages,
  pageIndex,
  threadIndex,
  onSelectPage,
  onSelectThread,
}: {
  readonly board: OpaqueObject;
  readonly boardIndex: number;
  readonly pages: readonly Page[];
  readonly pageIndex: number;
  readonly threadIndex?: number;
  readonly onSelectPage: (index: number) => void;
  readonly onSelectThread: (index: number) => void;
}) {
  const page = pages[pageIndex];
  if (page === undefined) {
    return null;
  }
  const selectedThreadIndex = validIndex(threadIndex, page.threads)
    ? threadIndex
    : undefined;

  return (
    <>
      <nav aria-label="Catalog pages" class="mt-4">
        <ol class="flex flex-wrap gap-2">
          {pages.map((candidate, index) => (
            <li key={index}>
              <button
                aria-current={pageIndex === index ? "page" : undefined}
                class="min-h-11 rounded-lg border border-slate-700 px-3 py-2 text-sm font-semibold hover:bg-slate-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400 aria-current:border-cyan-400 aria-current:text-cyan-300"
                onClick={() => onSelectPage(index)}
                type="button"
              >
                {pageLabel(candidate, index)}
              </button>
            </li>
          ))}
        </ol>
      </nav>

      {page.threads.length === 0 ? (
        <ResourceStatus heading="Empty page">
          No threads were observed on this catalog page.
        </ResourceStatus>
      ) : (
        <ol class="mt-4 grid gap-2">
          {page.threads.map((entry, index) => (
            <ThreadRow
              board={board}
              entry={entry}
              index={index}
              key={index}
              onSelect={() => onSelectThread(index)}
              selected={selectedThreadIndex === index}
            />
          ))}
        </ol>
      )}

      {selectedThreadIndex !== undefined && (
        <ThreadReader
          board={board}
          entry={page.threads[selectedThreadIndex]}
          key={`${boardIndex}:${pageIndex}:${selectedThreadIndex}`}
        />
      )}
    </>
  );
}

function ThreadRow({
  board,
  entry,
  index,
  selected,
  onSelect,
}: {
  readonly board: OpaqueObject;
  readonly entry: ThreadEntry;
  readonly index: number;
  readonly selected: boolean;
  readonly onSelect: () => void;
}) {
  const status = threadStatus(entry);
  const details = threadDetails(entry.summary);
  return (
    <li
      class={`grid min-h-11 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 rounded-lg border px-2 py-1 sm:grid-cols-[minmax(7rem,1fr)_minmax(10rem,2fr)_auto] sm:px-3 ${
        status === undefined
          ? "border-slate-800 bg-slate-950"
          : "border-amber-700 bg-amber-950/40"
      }`}
    >
      <button
        aria-pressed={selected}
        class="min-w-0 rounded-md px-2 py-2 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400 sm:col-span-2 sm:grid sm:grid-cols-subgrid"
        onClick={onSelect}
        type="button"
      >
        <span class="block truncate text-sm font-semibold">
          {threadLabel(entry, index)}
        </span>
        <span class="block truncate text-xs text-slate-400 sm:text-sm">
          {details ?? "Summary details unavailable"}
          {status !== undefined && ` · ${status}`}
        </span>
      </button>
      <CanonicalLink
        href={canonicalThreadURL(board, entry.summary)}
        label={`Open ${threadLabel(entry, index)} on 4chan`}
      />
    </li>
  );
}

function ResourceStatus({
  heading,
  children,
  degraded = false,
}: {
  readonly heading: string;
  readonly children: ComponentChildren;
  readonly degraded?: boolean;
}) {
  return (
    <div
      class={`mt-4 rounded-lg border p-4 ${
        degraded
          ? "border-amber-700 bg-amber-950/40 text-amber-100"
          : "border-slate-700 bg-slate-950 text-slate-200"
      }`}
      role="status"
    >
      <h4 class="font-semibold">{heading}</h4>
      <p class="mt-1 text-sm">{children}</p>
    </div>
  );
}

function CanonicalLink({
  href,
  label,
}: {
  readonly href?: string;
  readonly label: string;
}) {
  if (href === undefined) {
    return null;
  }
  return (
    <a
      aria-label={label}
      class="shrink-0 rounded-md px-2 py-2 text-xs font-semibold text-cyan-300 underline decoration-cyan-700 underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400"
      href={href}
    >
      4chan ↗
    </a>
  );
}

function boardLabel(item: BoardItem, index: number): string {
  const identifier = textField(item.board, "board");
  const title = textField(item.board, "title");
  if (identifier !== undefined && title !== undefined) {
    return `/${identifier}/ · ${title}`;
  }
  if (identifier !== undefined) {
    return `/${identifier}/`;
  }
  return title ?? `Board ${index + 1} · details unavailable`;
}

function catalogStatus(item: BoardItem): string | undefined {
  if (item.catalog === undefined) {
    return "Not available";
  }
  if (item.catalog.state === "failed") {
    return "Failed";
  }
  return undefined;
}

function pageLabel(page: Page, index: number): string {
  const upstream = scalarField(page.metadata, "page");
  return upstream === undefined ? `Page ${index + 1}` : `Page ${upstream}`;
}

function threadLabel(entry: ThreadEntry, index: number): string {
  const number = positiveIntegerField(entry.summary, "no");
  const subject =
    textField(entry.summary, "sub") ?? textField(entry.summary, "subject");
  if (number !== undefined && subject !== undefined) {
    return `No. ${number} · ${subject}`;
  }
  if (number !== undefined) {
    return `Thread No. ${number}`;
  }
  return subject ?? `Thread ${index + 1} · details unavailable`;
}

function threadDetails(summary: OpaqueObject): string | undefined {
  const details: string[] = [];
  const name = textField(summary, "name");
  const observed = textField(summary, "now");
  const replies = nonNegativeIntegerField(summary, "replies");
  const images = nonNegativeIntegerField(summary, "images");
  if (name !== undefined) {
    details.push(name);
  }
  if (observed !== undefined) {
    details.push(observed);
  }
  if (replies !== undefined) {
    details.push(`${replies} replies`);
  }
  if (images !== undefined) {
    details.push(`${images} images`);
  }
  return details.length === 0 ? undefined : details.join(" · ");
}

function threadStatus(entry: ThreadEntry): string | undefined {
  if (entry.thread === undefined) {
    return "Not available";
  }
  if (entry.thread.state === "failed") {
    return "Failed";
  }
  if (entry.thread.state === "oversize") {
    return "Truncated";
  }
  return undefined;
}

function selectedPageIndex(item: BoardItem, requested: number): number {
  return item.catalog?.state === "present" &&
    validIndex(requested, item.catalog.pages)
    ? requested
    : 0;
}

function validIndex(
  index: number | undefined,
  values: readonly unknown[],
): index is number {
  return (
    index !== undefined &&
    Number.isInteger(index) &&
    index >= 0 &&
    index < values.length
  );
}

function textField(object: OpaqueObject, field: string): string | undefined {
  const value = object[field];
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function scalarField(
  object: OpaqueObject,
  field: string,
): string | number | undefined {
  const value = object[field];
  if (typeof value === "string" && value.trim() !== "") {
    return value;
  }
  return typeof value === "number" && Number.isFinite(value)
    ? value
    : undefined;
}

function positiveIntegerField(
  object: OpaqueObject,
  field: string,
): number | undefined {
  const value = object[field];
  return Number.isSafeInteger(value) && Number(value) > 0
    ? Number(value)
    : undefined;
}

function nonNegativeIntegerField(
  object: OpaqueObject,
  field: string,
): number | undefined {
  const value = object[field];
  return Number.isSafeInteger(value) && Number(value) >= 0
    ? Number(value)
    : undefined;
}
