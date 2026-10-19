# Zero-Taxonomy Epistemic Workbench Architecture

Traditional personal knowledge management systems (Obsidian, Notion, Roam) fail in practice because they conflate **ephemeral scratch capture** with **verified long-term knowledge**, forcing users into performative librarian chores (folders, tags, `[[wikilinks]]`, and weekly reviews).

## Core Architectural Pattern

1. **Two-Tier Storage Separation:**
   - **The Quarry (`quarry/<ULID>.md`):** Flat directory of raw atomic stones (50–120 words). No titles, no frontmatter, no inline link syntax. Subject to exponential age weathering (`lambda = 0.20` unanchored, `lambda = 0.03` when anchored with an inline `(Author, Year)` citation or a trail in `trails.tsv`).
   - **The Monoliths (`monoliths/<slug>.md`):** Human-named, permanent syntheses ("The Spellbook") carved only after a set of stones has solved a real engineering or research problem. Monoliths never decay (`lambda = 0.00`) and receive a `1.20x` retrieval boost.
2. **Workbench Over Library (The Session Stack):**
   - Productivity requires holding 4–7 fragments in working memory simultaneously. The 3-pane TUI (`Horizon` | `Slate & Chisel` | `Cairn`) lets users push discovered stones onto an ordered session stack (`Cairn`), organize them under virtual headers, and continuously emit `.cairn/trail.md` for immediate consumption by coding agents or design docs.
3. **Disposable Derived Index (`quarry.db`):**
   - SQLite stores only derived state: FTS5 BM25 lexical indices (`notes_fts`), SHA-256 content hashes, mirrored `trails.tsv` edges, and active session stacks. Deleting `quarry.db` loses zero knowledge (`cairn index --rebuild`).
