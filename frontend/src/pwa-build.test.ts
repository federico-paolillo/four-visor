// This module validates the installable manifest, icons, and deterministic production shell contract.

import { build, type Plugin, type Rollup } from "vite";
import { describe, expect, it, vi } from "vitest";

import icon192DataUrl from "../public/icons/icon-192.png?inline";
import icon512DataUrl from "../public/icons/icon-512.png?inline";
import manifestSource from "../public/manifest.webmanifest?raw";
import { hashShellArtifacts, type ShellArtifact } from "../vite.config";

const fixedArtifacts = [
  {
    path: "/manifest.webmanifest",
    bytes: new TextEncoder().encode(manifestSource),
  },
  { path: "/icons/icon-192.png", bytes: pngBytes(icon192DataUrl) },
  { path: "/icons/icon-512.png", bytes: pngBytes(icon512DataUrl) },
] as const;

describe("PWA installability artifacts", () => {
  it("provides the required Chrome manifest contract", () => {
    const manifest: unknown = JSON.parse(manifestSource);

    expect(manifest).toEqual({
      id: "/",
      name: "4Visor",
      short_name: "4Visor",
      description: "A read-only, anonymous 4chan reader.",
      start_url: "/",
      scope: "/",
      display: "standalone",
      background_color: "#0f172a",
      theme_color: "#0f172a",
      prefer_related_applications: false,
      icons: [
        {
          src: "/icons/icon-192.png",
          sizes: "192x192",
          type: "image/png",
          purpose: "any",
        },
        {
          src: "/icons/icon-512.png",
          sizes: "512x512",
          type: "image/png",
          purpose: "any",
        },
      ],
    });
  });

  it.each([
    ["192x192", icon192DataUrl, 192],
    ["512x512", icon512DataUrl, 512],
  ])("provides a real %s PNG icon", (_, dataUrl, size) => {
    expect(dataUrl.startsWith("data:image/png;base64,")).toBe(true);
    expect(pngDimensions(pngBytes(dataUrl))).toEqual({
      width: size,
      height: size,
    });
  });

  it("changes the revision when any fixed shell artifact byte changes", async () => {
    const baseline = await hashShellArtifacts(fixedArtifacts);

    for (const [index, artifact] of fixedArtifacts.entries()) {
      const changedBytes = artifact.bytes.slice();
      changedBytes[changedBytes.length - 1] ^= 1;
      const changedArtifacts = fixedArtifacts.map(
        (candidate, candidateIndex) =>
          candidateIndex === index
            ? { path: candidate.path, bytes: changedBytes }
            : candidate,
      );
      expect(await hashShellArtifacts(changedArtifacts)).not.toBe(baseline);
    }
  });
});

it("emits one deterministic, closed shell-cache contract", async () => {
  vi.stubEnv("NODE_ENV", "production");
  try {
    const firstBuild = await productionBuild();
    const secondBuild = await productionBuild();
    expect(artifactContents(secondBuild)).toEqual(artifactContents(firstBuild));

    const worker = output(firstBuild, "service-worker.js");
    expect(worker.type).toBe("chunk");
    if (worker.type !== "chunk") {
      throw new Error("service worker output must be a chunk");
    }

    expect(worker.code).not.toContain("__FOURVISOR_");
    const cacheNames = worker.code.match(/four-visor-shell-[0-9a-f]{64}/g);
    expect(cacheNames).toHaveLength(1);

    const application = firstBuild.find(
      (candidate) =>
        candidate.type === "chunk" && candidate.fileName.startsWith("assets/"),
    );
    expect(application?.type).toBe("chunk");
    if (application?.type !== "chunk") {
      throw new Error("application output must be a chunk");
    }
    expect(application.code).toContain("/service-worker.js");
    expect(application.code).toContain("updateViaCache");
    expect(application.code).toContain('type:"module"');
    expect(application.code).toContain('scope:"/"');
    expect(application.code).toContain('updateViaCache:"none"');

    const generatedArtifacts = firstBuild
      .filter(
        (candidate) =>
          candidate.fileName === "index.html" ||
          (candidate.fileName.startsWith("assets/") &&
            [".js", ".css"].some((extension) =>
              candidate.fileName.endsWith(extension),
            )),
      )
      .map((candidate) => ({
        path: `/${candidate.fileName}`,
        bytes: outputBytes(candidate),
      }));
    const expectedArtifacts: ShellArtifact[] = [
      ...generatedArtifacts,
      ...fixedArtifacts,
    ];
    const expectedAssets = expectedArtifacts
      .map(({ path }) => path)
      .sort((left, right) => left.localeCompare(right));

    expect(injectedShellAssets(worker.code)).toEqual(expectedAssets);
    expect(cacheNames?.[0]).toBe(
      `four-visor-shell-${await hashShellArtifacts(expectedArtifacts)}`,
    );
    expect(expectedAssets).toContain("/index.html");
    expect(expectedAssets).toContain("/manifest.webmanifest");
    expect(expectedAssets).toContain("/icons/icon-192.png");
    expect(expectedAssets).toContain("/icons/icon-512.png");
    expect(expectedAssets.some((path) => path.endsWith(".js"))).toBe(true);
    expect(expectedAssets.some((path) => path.endsWith(".css"))).toBe(true);
    expect(expectedAssets).not.toContain("/service-worker.js");
    expect(
      expectedAssets.some(
        (path) =>
          path.endsWith(".map") ||
          path.startsWith("/api/") ||
          path.includes("snapshot") ||
          path.includes("4cdn.org") ||
          path.includes("4chan.org"),
      ),
    ).toBe(false);
  } finally {
    vi.unstubAllEnvs();
  }
}, 30_000);

it("excludes generated non-application assets from the shell precache", async () => {
  const probeFileName = "assets/generated-probe.bin";
  const probe: Plugin = {
    name: "generated-binary-probe",
    buildStart() {
      this.emitFile({
        type: "asset",
        fileName: probeFileName,
        source: new Uint8Array([0xde, 0xad, 0xbe, 0xef]),
      });
    },
  };

  vi.stubEnv("NODE_ENV", "production");
  try {
    const buildOutputs = await productionBuild([probe]);
    expect(output(buildOutputs, probeFileName).type).toBe("asset");

    const worker = output(buildOutputs, "service-worker.js");
    expect(worker.type).toBe("chunk");
    if (worker.type !== "chunk") {
      throw new Error("service worker output must be a chunk");
    }
    expect(injectedShellAssets(worker.code)).not.toContain(`/${probeFileName}`);
  } finally {
    vi.unstubAllEnvs();
  }
}, 30_000);

type BuildOutput = Rollup.OutputAsset | Rollup.OutputChunk;

async function productionBuild(plugins: Plugin[] = []): Promise<BuildOutput[]> {
  const result = await build({
    configFile: "vite.config.ts",
    logLevel: "silent",
    mode: "production",
    plugins,
    build: { write: false },
  });
  const builds = (Array.isArray(result) ? result : [result]) as {
    output: BuildOutput[];
  }[];
  return builds.flatMap(({ output: buildOutput }) => buildOutput);
}

function output(outputs: BuildOutput[], fileName: string): BuildOutput {
  const selected = outputs.find((candidate) => candidate.fileName === fileName);
  if (selected === undefined) {
    throw new Error(`missing build output ${fileName}`);
  }
  return selected;
}

function outputBytes(candidate: BuildOutput): Uint8Array {
  if (candidate.type === "chunk") {
    return new TextEncoder().encode(candidate.code);
  }
  return typeof candidate.source === "string"
    ? new TextEncoder().encode(candidate.source)
    : candidate.source;
}

function artifactContents(outputs: BuildOutput[]): Record<string, string> {
  return Object.fromEntries(
    outputs
      .map((candidate) => [
        candidate.fileName,
        Array.from(outputBytes(candidate)).join(","),
      ])
      .sort(([left], [right]) => left.localeCompare(right)),
  );
}

function injectedShellAssets(workerCode: string): string[] {
  const stringArguments = workerCode.matchAll(
    /JSON\.parse\(((?:"(?:\\.|[^"\\])*")|(?:'(?:\\.|[^'\\])*'))\)/g,
  );
  for (const match of stringArguments) {
    const outer = match[1];
    if (outer === undefined || outer.startsWith("'")) {
      continue;
    }
    const inner: unknown = JSON.parse(outer);
    if (typeof inner !== "string") {
      continue;
    }
    const candidate: unknown = JSON.parse(inner);
    if (
      Array.isArray(candidate) &&
      candidate.every((path) => typeof path === "string")
    ) {
      return candidate;
    }
  }
  throw new Error("generated worker has no shell asset configuration");
}

function pngBytes(dataUrl: string): Uint8Array {
  const payload = dataUrl.split(",", 2)[1];
  if (payload === undefined) {
    throw new Error("PNG data URL has no payload");
  }
  return Uint8Array.from(atob(payload), (character) => character.charCodeAt(0));
}

function pngDimensions(bytes: Uint8Array): {
  height: number;
  width: number;
} {
  expect([...bytes.slice(0, 8)]).toEqual([137, 80, 78, 71, 13, 10, 26, 10]);
  expect(new TextDecoder().decode(bytes.slice(12, 16))).toBe("IHDR");
  const header = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return { width: header.getUint32(16), height: header.getUint32(20) };
}
