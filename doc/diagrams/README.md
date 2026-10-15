# Documentation diagrams

Diagram sources for the documentation site, in [Mermaid](https://mermaid.js.org/)
format (`.mmd`). They are not published as files; a page pulls one in with a
snippet line inside a `mermaid` code fence:

    ```mermaid
    --8<-- "diagrams/policy-lifecycle.mmd"
    ```

Material for MkDocs renders the diagram in the browser and matches the light
and dark color schemes automatically. A missing source file fails the build.

Edit a diagram with any text editor, preview it at https://mermaid.live, and
check the result with `mkdocs serve`. To export a static SVG (for slides or
the website), use the Mermaid CLI:

    npx -p @mermaid-js/mermaid-cli mmdc -i policy-lifecycle.mmd -o policy-lifecycle.svg

| Source | Used in |
|---|---|
| `architecture-overview.mmd` | `index.md` |
| `architecture-detail.mmd` | `getting-started/concepts.md` |
| `policy-lifecycle.mmd` | `getting-started/concepts.md` |
| `recovery-key-lifecycle.mmd` | `policy-types/disk-encryption.md` |
| `dev-release-pipeline.mmd` | `development/building.md` |
| `dev-components.mmd` | `development/architecture.md` |
| `dev-enrollment.mmd` | `development/architecture.md` |
| `dev-policy-stream.mmd` | `development/architecture.md` |
| `dev-cert-renewal.mmd` | `development/architecture.md` |
| `dev-frontend-request.mmd` | `development/frontend.md` |
| `dev-policy-type-touchpoints.mmd` | `development/adding-a-policy-type.md` |
| `dev-extension-host.mmd` | `development/editions.md` |
