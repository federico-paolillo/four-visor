// This module configures the browser build and injects its exact shell precache contract.

import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

const cacheNameMarker = "__FOURVISOR_SHELL_CACHE_NAME__";
const shellAssetsMarker = "__FOURVISOR_SHELL_ASSETS__";
const serviceWorkerFileName = "service-worker.js";
const fixedShellAssetPaths = [
  "/manifest.webmanifest",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
] as const;

export type ShellArtifact = {
  readonly bytes: Uint8Array;
  readonly path: string;
};

export async function hashShellArtifacts(
  artifacts: readonly ShellArtifact[],
): Promise<string> {
  const encoder = new TextEncoder();
  const framedParts: Uint8Array[] = [];
  let byteLength = 0;

  for (const artifact of [...artifacts].sort((left, right) =>
    left.path.localeCompare(right.path),
  )) {
    for (const part of [encoder.encode(artifact.path), artifact.bytes]) {
      const length = encoder.encode(`${part.byteLength}:`);
      framedParts.push(length, part);
      byteLength += length.byteLength + part.byteLength;
    }
  }

  const framedArtifacts = new Uint8Array(byteLength);
  let offset = 0;
  for (const part of framedParts) {
    framedArtifacts.set(part, offset);
    offset += part.byteLength;
  }

  const digest = await crypto.subtle.digest("SHA-256", framedArtifacts);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

function shellPrecache(): Plugin {
  let projectRoot = "";

  return {
    name: "four-visor-shell-precache",
    apply: "build",
    enforce: "post",
    configResolved(config) {
      projectRoot = config.root;
    },
    async generateBundle(_, bundle) {
      const worker = bundle[serviceWorkerFileName];
      if (worker?.type !== "chunk") {
        this.error(`missing ${serviceWorkerFileName} output chunk`);
      }
      if (worker.imports.length !== 0 || worker.dynamicImports.length !== 0) {
        this.error(`${serviceWorkerFileName} must be a standalone module`);
      }

      const generatedShellOutputs = Object.values(bundle)
        .filter(
          (output) =>
            output.fileName === "index.html" ||
            (output.fileName.startsWith("assets/") &&
              [".js", ".css"].some((extension) =>
                output.fileName.endsWith(extension),
              )),
        )
        .sort((left, right) => left.fileName.localeCompare(right.fileName));
      if (
        generatedShellOutputs.filter(
          (output) => output.fileName === "index.html",
        ).length !== 1
      ) {
        this.error("build must emit exactly one index.html shell entry");
      }
      for (const extension of [".js", ".css"]) {
        if (
          !generatedShellOutputs.some((output) =>
            output.fileName.endsWith(extension),
          )
        ) {
          this.error(`build must emit a shell ${extension} asset`);
        }
      }

      const readFileModule = "node:fs/promises";
      const { readFile } = (await import(readFileModule)) as {
        readFile(path: string): Promise<Uint8Array>;
      };
      const fixedShellArtifacts = await Promise.all(
        fixedShellAssetPaths.map(async (path) => ({
          path,
          bytes: await readFile(`${projectRoot}/public${path}`),
        })),
      );
      const generatedShellArtifacts = generatedShellOutputs.map((output) => ({
        path: `/${output.fileName}`,
        bytes: outputBytes(output),
      }));
      const shellArtifacts = [
        ...generatedShellArtifacts,
        ...fixedShellArtifacts,
      ].sort((left, right) => left.path.localeCompare(right.path));
      const shellAssets = shellArtifacts.map(({ path }) => path);
      if (new Set(shellAssets).size !== shellAssets.length) {
        this.error("shell precache paths must be unique");
      }

      const revision = await hashShellArtifacts(shellArtifacts);
      worker.code = replaceExactlyOnce(
        worker.code,
        cacheNameMarker,
        `four-visor-shell-${revision}`,
      );
      worker.code = replaceExactlyOnce(
        worker.code,
        shellAssetsMarker,
        escapeJavaScriptString(JSON.stringify(shellAssets)),
      );
    },
  };
}

function outputBytes(output: {
  readonly code?: string;
  readonly source?: string | Uint8Array;
}): Uint8Array {
  if (output.code !== undefined) {
    return new TextEncoder().encode(output.code);
  }
  if (typeof output.source === "string") {
    return new TextEncoder().encode(output.source);
  }
  if (output.source instanceof Uint8Array) {
    return output.source;
  }
  throw new Error("shell output has no byte content");
}

function replaceExactlyOnce(
  source: string,
  marker: string,
  replacement: string,
): string {
  const firstMarker = source.indexOf(marker);
  if (
    firstMarker === -1 ||
    source.indexOf(marker, firstMarker + marker.length) !== -1
  ) {
    throw new Error(`${marker} must occur exactly once`);
  }
  return `${source.slice(0, firstMarker)}${replacement}${source.slice(
    firstMarker + marker.length,
  )}`;
}

function escapeJavaScriptString(value: string): string {
  return JSON.stringify(value).slice(1, -1);
}

export default defineConfig({
  plugins: [preact(), tailwindcss(), shellPrecache()],
  build: {
    target: "chrome150",
    rollupOptions: {
      input: {
        app: "index.html",
        "service-worker": "src/service-worker.ts",
      },
      output: {
        entryFileNames: ({ name }) =>
          name === "service-worker"
            ? serviceWorkerFileName
            : "assets/[name]-[hash].js",
      },
    },
  },
  test: {
    passWithNoTests: true,
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:65102",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
});
