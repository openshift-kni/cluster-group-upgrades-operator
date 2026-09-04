# FBC catalogs

Catalogs are generated at Konflux build time and must not be committed.

There are two catalogs on this branch:

- `5.0/` — 5.0 FBC (`topology-aware-lifecycle-manager-fbc-5-0`). Includes the released 4.22 bundles plus the 5.0 placeholder last bundle; Konflux fills that placeholder from `bundle.builds.in.yaml`.
- `4.22/` — same 5.0 FBC component (`topology-aware-lifecycle-manager-fbc-5-0`), built from this directory so 4.22 z-streams can be released into the 5.0 catalog without the 5.0 placeholder. Released 4.22.0 is static (no placeholder). Static 5.0 bundles will be added here over time.

When a new 4.22 version is released, add it to **both** `catalog-template.in.yaml` files (`4.22/` and `5.0/`).

Generated files (`catalog-template.out.yaml`, `topology-aware-lifecycle-manager/catalog.json`) are listed in `.gitignore`.
