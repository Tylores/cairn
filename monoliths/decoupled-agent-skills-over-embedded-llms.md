# Decoupled Agent Skills Over Embedded LLM Pipelines

Hardcoding generative LLM prompts, embedding daemons, and classification schemas inside a compiled CLI binary creates brittle software: every prompt tweak requires recompilation, and background daemons introduce latency and failure modes into simple file operations.

## The Three-Boundary Split

1. **Deterministic Go Binary (`cairn`):**
   - Zero network calls and zero embedded AI. Performs only fast, predictable local operations: spawning `$EDITOR` (`cairn drop`), computing FTS5 BM25 + token triangulation + weathering (`cairn query`), running the Bubble Tea workbench (`cairn`), and writing `.cairn/trail.md` (`cairn blaze`).
2. **Passive Context Mirror (`.cairn/trail.md`):**
   - Every stack mutation in the TUI writes clean Markdown with invisible `<!-- stone:<ID> -->` provenance comments to `.cairn/trail.md`. Terminal coding agents can `@.cairn/trail.md` directly without protocol overhead or clipboard copy-pasting.
3. **On-Demand Agent Skills (`cairn-rockhound`, `cairn-link`, `cairn-carve`, `cairn-sift`):**
   - Source skimming, semantic discovery, and epistemic maintenance live in editable `.md` skill files executed by the user's terminal agent only when requested.
