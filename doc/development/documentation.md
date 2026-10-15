# Writing documentation

The documentation you are reading is built from Markdown files in the
`doc/` directory of the repository with [MkDocs](https://www.mkdocs.org/)
and the Material for MkDocs theme. This page explains how the site is
built, how pages are organized, the style rules, and how diagrams and
screenshots are produced.

## How the site is built

| File or directory | Purpose |
|---|---|
| `mkdocs.yml` (repository root) | Site configuration: `docs_dir: doc`, theme, Markdown extensions, navigation |
| `doc/` | The pages, one Markdown file per page |
| `doc/requirements.txt` | Python requirements: `mkdocs>=1.6`, `mkdocs-material>=9.5` |
| `doc/diagrams/` | Mermaid diagram sources (`*.mmd`), pulled into pages; not published as files |
| `doc/images/screenshots/` | Web UI screenshots (WebP) |
| `doc/tools/screenshots/` | The screenshot script; not published |

`mkdocs.yml` excludes `requirements.txt`, `diagrams/` and `tools/` from the
published site (`exclude_docs`).

Install the tools once, preferably in a virtual environment:

```bash
python3 -m venv ~/.venvs/bor-docs
. ~/.venvs/bor-docs/bin/activate
pip install -r doc/requirements.txt
```

Then, from the repository root:

```bash
mkdocs serve               # live preview on http://127.0.0.1:8000
mkdocs build --strict      # full build into site/; warnings are errors
```

Always run `mkdocs build --strict` before you push a documentation change.
In strict mode a broken link to another page, a link to an anchor that does
not exist, a page missing from the navigation, or a missing diagram source
fails the build.

### Theme and extensions

The site uses the `material` theme with light and dark palettes, top-level
navigation tabs, sections, search and copy buttons on code blocks. These
Markdown extensions are enabled and are the only syntax to rely on:
`abbr`, `admonition`, `attr_list`, `def_list`, `footnotes`, `md_in_html`,
`tables`, `toc` (with permalinks), `pymdownx.details`,
`pymdownx.highlight`, `pymdownx.inlinehilite`, `pymdownx.keys`,
`pymdownx.snippets` (base path `doc`, `check_paths: true`),
`pymdownx.superfences` (with the `mermaid` custom fence) and
`pymdownx.tabbed` (alternate style).

## Page structure and navigation

The sections of the site are directories under `doc/`:

| Directory | Content |
|---|---|
| `getting-started/` | Concepts and the quick start |
| `installation/` | Requirements, server and agent installation, upgrades |
| `configuration/` | Server, agent, TLS, LDAP, Kerberos, MFA |
| `user-guide/` | The web UI, screen by screen |
| `policy-types/` | One page per policy type, plus an overview |
| `operations/` | Monitoring, backup, troubleshooting |
| `security/` | Compliance and hardening |
| `reference/` | Files and ports, command line, permissions |
| `development/` | This section: contributor documentation |

Every page must appear in the `nav` of `mkdocs.yml`; a new page is not
visible otherwise and the strict build warns about it. A section's
`index.md`, listed first without a title, becomes the section's landing
page (`navigation.indexes`).

Each page has:

- exactly one `# H1`, which is the page title;
- a short introduction saying what the page covers;
- task-oriented sections ("Create a node group", "Add a policy type"),
  numbered steps for procedures and tables for references.

Link to other pages with relative paths to the `.md` file, never to the
published URL, and only to pages that are in the navigation:

```markdown
See [policy bindings](../user-guide/bindings.md#create-a-binding).
```

Never link to files under `docs/` in the repository: those are internal
working notes, not part of the site.

## Style rules

Text
:   American English, ASCII only. Never use the Unicode arrow (U+2192) or
    the em and en dashes (U+2014, U+2013): write `->` for arrows and a
    plain `-` for dashes and ranges. Avoid curly quotes and the ellipsis
    character as well. This check finds offenders:

    ```bash
    grep -rlP '[^\x00-\x7F]' doc/
    ```

UI labels
:   **Bold**, copied exactly from the web UI source, with `->` for
    navigation paths: **Policies -> Create a policy**,
    **Settings -> Agent Notifications**.

Code, paths, keys and commands
:   `Inline code`. Fenced blocks always name a language (`bash`, `yaml`,
    `json`, `text`, `go`, `tsx`, `proto`, ...). Commands that need root
    use `sudo`.

Admonitions
:   For things the reader must not miss:

    ```markdown
    !!! warning "Back up the CA first"
        Text indented by four spaces.

    ??? note "Collapsed by default"
        Details for the curious.
    ```

    Use `note`, `tip`, `warning` and `danger`; reserve `danger` for data
    loss and lockouts.

Content tabs
:   For alternatives such as distribution families, with the content
    indented by four spaces:

    ````markdown
    === "Debian / Ubuntu"

        ```bash
        sudo apt install bor-agent
        ```

    === "Fedora / RHEL"

        ```bash
        sudo dnf install bor-agent
        ```
    ````

Examples
:   Use `bor.example.com` as the server name, addresses from the
    documentation ranges (`192.0.2.0/24`, `2001:db8::/32`), and invented
    user and group names. Never real data.

Ports
:   The web UI, REST API and enrollment port is `server.enrollment_port`
    (`BOR_ENROLLMENT_PORT`); its default is 8443, and production
    deployments are recommended to use 443. Do not present 8443 as fixed:
    write "the web port" or "the enrollment port
    (`server.enrollment_port`, default 8443)", and use
    `https://bor.example.com` without a port in examples. The quick start
    keeps the default 8443, and reference tables state the default plus
    the recommendation. The agent policy port (`server.policy_port`,
    default 8444) is separate.

Accuracy
:   Every fact - configuration key, default, path, port, flag, UI label,
    permission name, behavior - is verified against the code in the same
    tree before it is written. If something cannot be verified, leave it
    out. Do not document features that do not exist yet or that exist only
    in other distributions.

## Diagrams

Diagrams are [Mermaid](https://mermaid.js.org/) sources in `doc/diagrams/`,
one diagram per `.mmd` file. A page includes one with a snippet line inside
a `mermaid` fence:

````markdown
```mermaid
--8<-- "diagrams/policy-lifecycle.mmd"
```
````

Material renders the diagram in the browser and follows the light and dark
color schemes. Because `check_paths` is on, a missing source fails the
build.

- Keep diagrams small: a sequence diagram for a flow, a flowchart for a
  structure, a state diagram for a lifecycle.
- Use plain ASCII in labels; write words instead of angle brackets (for
  example `NAME` rather than `<name>`).
- Preview a diagram at <https://mermaid.live> or with `mkdocs serve`.
- Pages in the development section prefix their diagram files with `dev-`.
- Add a row for every new diagram to the table in `doc/diagrams/README.md`
  ("Source" and "Used in").

To export a diagram as SVG for slides or the website, use the Mermaid
CLI:

```bash
npx -p @mermaid-js/mermaid-cli mmdc -i policy-lifecycle.mmd -o policy-lifecycle.svg
```

## Screenshots

Screenshots of the web UI live in `doc/images/screenshots/` as WebP files
with stable names (`dashboard.webp`, `policy-editor-polkit.webp`, ...).
They are produced by a script, never captured by hand, so the whole set
can be refreshed before a release and the diff shows what changed.

The script is `doc/tools/screenshots/doc-shoot.js` (Node.js and
`puppeteer-core`, driving a local Chrome or Chromium). It captures every
screenshot at 1280x800 CSS pixels, 2x device scale, in the light theme,
and logs in through the real login page. Its `README.md` has the complete
instructions; in short:

1. Start a **throwaway** server with a fresh PostgreSQL, built from the
   current tree, and seed it with the demo kit (`demo/reset/seed.sh` with
   `SEED_DIR=demo/seed`, then `demo/reset/extras.sql`), so no screen is
   empty. Never point the script at a real installation: the images are
   published.
2. Install the script's dependencies once:

    ```bash
    cd doc/tools/screenshots
    npm install
    ```

3. Capture all screenshots, or only some with `SHOTS`:

    ```bash
    node doc-shoot.js admin '<admin-password>'
    SHOTS=dashboard,nodes node doc-shoot.js admin '<admin-password>'
    ```

    `BOR_API` (default `https://localhost:8443`) points at the server;
    the browser shows the host name `BOR_UI_HOST` (default
    `bor.example.com`, resolved to 127.0.0.1), so no real host name
    appears in the images.

The script copies one released demo policy per type into a temporary draft
for the editor screenshots and deletes the drafts afterwards, answers the
agent package manifest and the enrollment token request with fake data,
and never submits the recovery-key reveal form. A new policy type needs an
entry in `EDITOR_SHOTS` (see
[Adding a policy type](adding-a-policy-type.md#demo-data-and-screenshots)).

Before committing, look at every image: no real host names, user names,
addresses or secrets.

Embed a screenshot with alt text that describes what it shows, and lazy
loading:

```markdown
![Polkit policy editor showing a rule with its description, exact action IDs and the action prefix org.freedesktop.udisks2.](../images/screenshots/policy-editor-polkit.webp){ loading=lazy }
```

Alt text is required. It describes the content of the screen for a reader
who cannot see it, not just its name.

## When to update the documentation

Documentation changes go in the **same pull request** as the behavior they
describe. In particular, update the docs when you:

- add or change a configuration key, default, flag, path or port;
- add, rename or move a UI label, screen or permission (and re-take the
  affected screenshots);
- add or change a policy type (its page under `doc/policy-types/`, the
  overview table, the navigation and the editor screenshot);
- change what the agent writes on a node or how compliance is reported;
- change the build, the protocol, the database schema or an extension
  point described in this development section.

Reviewers check that the pull request builds with
`mkdocs build --strict` and that the pages match the code.
