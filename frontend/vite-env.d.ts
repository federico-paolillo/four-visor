/// <reference types="vite/client" />

// This declaration enables Vite asset and import-meta types with strict environment keys.
interface ViteTypeOptions {
  strictImportMetaEnv: unknown;
}

// This query keeps the shared cache policy duplicated across isolated build entries.
declare module "*?application" {
  export function ownedShellCacheNames(cacheNames: readonly string[]): string[];
}
