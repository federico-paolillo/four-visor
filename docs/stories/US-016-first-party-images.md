# US-016: Package the backend and PWA as hardened first-party images

## Goal

Produce the two project-owned Linux-amd64 runtime images required by the deployment model: Go backend and frontend Caddy/static assets.

## User Value

The Operator can build small, deterministic runtime artifacts that serve 4Visor without shells, root privileges, or writable root filesystems.

## Scope

- Build the Go backend as a Linux amd64 static runtime artifact in a project-owned distroless image that runs as a non-root user and supports a read-only root filesystem.
- Build the production PWA and serve its assets with Caddy in a separate project-owned distroless/rootless/read-only-compatible image.
- Configure frontend Caddy only for static shell/assets; it neither proxies `/api` nor owns Brotli compression.
- Assign every listener and container port used by the two first-party images from the project range 65100–65199.
- Keep runtime writes out of project-owned root filesystems and document image build, architecture, user, filesystem, and runtime configuration assumptions.
- Do not rebuild or impose project-owned hardening on Memcached, the OpenTelemetry Collector, or other third-party images.

## Out of Scope

- Compose topology/host ports, edge routing, TLS, ingress configuration, multi-architecture images, registry publication, signing/SBOM systems, or enterprise container hardening.

## Dependencies

- US-007.
- US-011.
- US-015.

## Related MADRs

- None. The first-party hardening/runtime topology is locked; exact maintained bases and Dockerfile mechanics are local implementation choices.

## Traceability

- `Full Requirements / Deployment` and `/ Platform and testing` (`docs/SEED.md:191-217`): separate services, frontend Caddy/backend direct server, first-party hardening, and Linux amd64.
- `Deployment View / Container model` (`docs/SEED.md:1202-1229`): project versus third-party images and native configuration boundaries.
- `Technology Stack / Infrastructure` and `/ Operating systems` (`docs/SEED.md:1799-1807`, `1842-1851`): Docker, Caddy, distroless/rootless/read-only, and amd64.
- `Technology Rationale / First-party container hardening` (`docs/SEED.md:2027-2043`): reduced attack surface and third-party exemption.
- `Locked Decisions / Backend` (`docs/SEED.md:2169-2191`): separate containers, ingress compression ownership, hardening, and target architecture.

## Acceptance Criteria

1. Both first-party images build for Linux amd64 from repository sources and contain only their required runtime artifact/configuration.
2. Image metadata/runtime configuration uses a non-root user, and each service can perform its normal work with a read-only root filesystem and no shell-dependent startup.
3. The backend image exposes the Go server directly; the frontend image serves built assets through Caddy and contains no API proxy/Brotli responsibility.
4. Third-party images are referenced upstream rather than rebuilt merely for distroless/rootless/read-only controls.
5. Operator documentation states supported architecture and the required runtime mounts/environment without claiming multi-architecture or enterprise hardening.
6. Every configured backend or frontend listener/container port is in 65100–65199.

## Validation

- Integration validation builds each image, inspects configured user/architecture/entrypoint, and runs the contained process under a read-only filesystem in isolation; this is artifact integration validation, not a deployment/smoke test.
- Run the backend and frontend build/unit checks before image assembly.
