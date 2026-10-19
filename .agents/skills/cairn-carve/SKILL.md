---
name: cairn-carve
description: Distill the active .cairn/trail.md session stack into a permanent Monolith in monoliths/<slug>.md and archive consumed raw quarry stones. Use when the user asks to carve a monolith or crystallize the current Cairn trail.
---

# Cairn Carve (`cairn-carve`)

Crystallize the active `.cairn/trail.md` working session into a permanent, human-named Monolith in `monoliths/<slug>.md` and archive the consumed raw stones.

## Workflow

1. **Inspect the Active Trail:**
   - Read `.cairn/trail.md`.
   - Extract the `> **Chisel:**` thesis, section headers (`## ...`), and all source `<ULID>`s from `<!-- stone:<ULID> -->` provenance comments.

2. **Draft the Monolith:**
   - Synthesize the `> **Chisel:**` directive and stacked stones into a permanent, self-contained architectural pattern document with a clear `# Title` and structured sections.
   - **Preserve Provenance:** Retain all inline `(Author, Year)` and `(Spec §X.Y)` citations verbatim.
   - **Strip Ephemeral Scaffolding:** Remove all `<!-- stone:... -->` HTML comments from the synthesized Monolith prose.

3. **Confirm Before Carving:**
   - Present the proposed target filename `monoliths/<slug>.md`, the drafted Markdown content, and the list of raw `quarry/<ULID>.md` stones that will be archived.
   - Wait for explicit user confirmation.

4. **Execute the Carve:**
   - Write `monoliths/<slug>.md`.
   - Move consumed raw `quarry/<ULID>.md` files into `quarry/.trash/<ULID>.md` (never delete or move existing `monoliths/*.md` files).
   - Update `trails.tsv` so any edges referencing the consumed `<ULID>`s now point to `monolith:<slug>` (deduplicating self-loops or duplicate lines).
   - Rebuild/sync the index:
     ```bash
     ./cairn index
     ```
