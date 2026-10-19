# Cairn: bootstrap-cairn-mvp

> **Chisel:** Bootstrap the local-first Cairn CLI/TUI workbench in pure Go (`CGO_ENABLED=0`) with a disposable SQLite FTS5 index, three-tier epistemic weathering, and decoupled Agent Skills.

## 1. Architecture & Storage Rules

<!-- stone:monolith:zero-taxonomy-epistemic-workbench -->
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

<!-- stone:monolith:decoupled-agent-skills-over-embedded-llms -->
# Decoupled Agent Skills Over Embedded LLM Pipelines

Hardcoding generative LLM prompts, embedding daemons, and classification schemas inside a compiled CLI binary creates brittle software: every prompt tweak requires recompilation, and background daemons introduce latency and failure modes into simple file operations.

## The Three-Boundary Split

1. **Deterministic Go Binary (`cairn`):**
   - Zero network calls and zero embedded AI. Performs only fast, predictable local operations: spawning `$EDITOR` (`cairn drop`), computing FTS5 BM25 + token triangulation + weathering (`cairn query`), running the Bubble Tea workbench (`cairn`), and writing `.cairn/trail.md` (`cairn blaze`).
2. **Passive Context Mirror (`.cairn/trail.md`):**
   - Every stack mutation in the TUI writes clean Markdown with invisible `<!-- stone:<ID> -->` provenance comments to `.cairn/trail.md`. Terminal coding agents can `@.cairn/trail.md` directly without protocol overhead or clipboard copy-pasting.
3. **On-Demand Agent Skills (`cairn-rockhound`, `cairn-link`, `cairn-carve`, `cairn-sift`):**
   - Source skimming, semantic discovery, and epistemic maintenance live in editable `.md` skill files executed by the user's terminal agent only when requested.

<!-- stone:01K7P1G2P4O6I8U0Y2T4R6E8W0 -->
When configuring SQLite FTS5 with an external content table (content='notes'), the virtual index does not update automatically when rows in notes change. You must define AFTER INSERT, AFTER DELETE, and AFTER UPDATE triggers that explicitly issue the 'delete' command to notes_fts before inserting the updated rowid and body.

## 2. Retrieval & TUI Mechanics

<!-- stone:01K7P1D6H5G7F9D1S3A5P7O9I1 -->
Lowering a search threshold to surface "looser" ideas during brainstorming floods the UI with weak, generic noise. Instead, computing the penalized harmonic mean of token overlap between two stacked anchors—BridgeScore(c, A, B) = (2*sA*sB)/(sA+sB) - 0.25*|sA - sB|—surfaces structural stepping stones that connect separate thoughts without repeating direct matches.

<!-- stone:01K7P1F0Z2X4C6V8B0N2M4L6K8 -->
Embedding a full text editor inside a terminal UI duplicates Vim/Neovim poorly and breaks user muscle memory. In Go's Bubble Tea framework, tea.ExecProcess cleanly suspends the alternate screen buffer, hands the raw TTY to $EDITOR for native editing, and triggers an asynchronous re-index message the moment the editor process exits.
