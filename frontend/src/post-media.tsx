// This module renders validated attachments through direct native browser media behavior.

import { useState } from "preact/hooks";

import { type CanonicalPostMedia, canonicalPostMedia } from "./fourchan-url";
import type { OpaqueObject } from "./snapshot";

// PostMedia keeps optional attachment failures independent from stored post text.
export function PostMedia({
  board,
  post,
}: {
  readonly board: OpaqueObject;
  readonly post: OpaqueObject;
}) {
  const media = canonicalPostMedia(board, post);
  return media === undefined ? null : <ValidPostMedia media={media} />;
}

function ValidPostMedia({ media }: { readonly media: CanonicalPostMedia }) {
  const [revealed, setRevealed] = useState(!media.spoiler);
  if (!revealed) {
    return (
      <button
        class="mt-3 min-h-11 rounded-lg border border-slate-600 px-3 py-2 text-sm font-semibold text-cyan-300 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400"
        onClick={() => setRevealed(true)}
        type="button"
      >
        Reveal spoiler media
      </button>
    );
  }

  return (
    <section
      aria-label={`Media attachment ${media.filename}`}
      class="mt-3 min-w-0 rounded-lg border border-slate-700 bg-slate-950 p-3"
    >
      <p class="break-all text-xs text-slate-300">{media.filename}</p>
      <Thumbnail media={media} />
      <FullMedia media={media} />
    </section>
  );
}

function Thumbnail({ media }: { readonly media: CanonicalPostMedia }) {
  const [state, setState] = useState<"failed" | "loading">(() =>
    navigator.onLine ? "loading" : "failed",
  );
  if (state === "failed") {
    return (
      <MediaPlaceholder
        onRetry={() => setState("loading")}
        retryLabel={`Retry thumbnail ${media.filename}`}
      />
    );
  }

  return (
    <img
      alt={`Thumbnail for ${media.filename}`}
      class="mt-2 h-auto max-h-64 max-w-full rounded object-contain"
      height={media.thumbnailHeight}
      onError={() => setState("failed")}
      src={media.thumbnailURL}
      width={media.thumbnailWidth}
    />
  );
}

function FullMedia({ media }: { readonly media: CanonicalPostMedia }) {
  if (media.kind === "file") {
    return (
      <a
        class="mt-3 inline-flex min-h-11 items-center rounded-lg px-3 py-2 text-sm font-semibold text-cyan-300 underline decoration-cyan-700 underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400"
        href={media.fullURL}
        rel="noopener noreferrer"
        target="_blank"
        aria-label={`Open original file ${media.filename}`}
      >
        Open original file
      </a>
    );
  }

  return <EmbeddableMedia media={media} />;
}

function EmbeddableMedia({ media }: { readonly media: CanonicalPostMedia }) {
  const [state, setState] = useState<"closed" | "failed" | "open">("closed");
  const request = () => setState("open");

  if (state === "closed") {
    return (
      <button
        class="mt-3 min-h-11 rounded-lg bg-cyan-500 px-3 py-2 text-sm font-semibold text-slate-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400"
        onClick={request}
        type="button"
      >
        Load full {media.kind}
      </button>
    );
  }
  if (state === "failed") {
    return (
      <MediaPlaceholder
        onRetry={request}
        retryLabel={`Retry full ${media.kind} ${media.filename}`}
      />
    );
  }

  const common = {
    "aria-label": `Full ${media.kind} ${media.filename}`,
    class: "mt-3 max-h-[80vh] max-w-full rounded",
    onError: () => setState("failed"),
    src: media.fullURL,
  };
  if (media.kind === "image") {
    return (
      <img
        {...common}
        alt={media.filename}
        height={media.height}
        width={media.width}
      />
    );
  }
  if (media.kind === "video") {
    return (
      <video
        {...common}
        controls
        height={media.height}
        preload="none"
        width={media.width}
      />
    );
  }
  return <audio {...common} controls preload="none" />;
}

function MediaPlaceholder({
  onRetry,
  retryLabel,
}: {
  readonly onRetry: () => void;
  readonly retryLabel: string;
}) {
  return (
    <div
      class="mt-2 rounded-lg border border-slate-600 bg-slate-900 p-3 text-sm text-slate-200"
      role="status"
    >
      <p>
        <span aria-hidden="true">▧ </span>
        Media unavailable or offline.
      </p>
      <button
        aria-label={retryLabel}
        class="mt-2 min-h-11 rounded-lg border border-slate-500 px-3 py-2 font-semibold text-cyan-300 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-cyan-400"
        onClick={onRetry}
        type="button"
      >
        Retry media
      </button>
    </div>
  );
}
