---
name: cairn-rockhound
description: Skim a paper, specification, blog post, or URL to pan for high-signal candidate Stones calibrated against existing Cairn monoliths and the active trail. Use when the user asks to rockhound, prospect, or extract candidate stones from a source before a deep dive.
---

# Cairn Rockhound (`cairn-rockhound`)

Prospect an external document, specification, paper, or URL for high-signal atomic candidate Stones calibrated against your existing Cairn knowledge base.

## Workflow

1. **Calibrate Against Existing Knowledge:**
   - Read `.cairn/trail.md` (if present) to understand the active problem context and `> **Chisel:**` thesis.
   - Run `./cairn query "<core source terms>" --limit=8` to inspect existing Monoliths and Quarry Stones that overlap with the source.

2. **Filter Out River Gravel:**
   - Discard introductory definitions, generic textbook background, marketing boilerplate, and concepts already captured in existing Monoliths or Stones.
   - Extract only non-obvious mechanisms, concrete architectural trade-offs, subtle failure modes, or empirical claims that extend or challenge existing stones.

3. **Present the Prospecting Report:**
   - **Deep-Dive Verdict:** Output a concise 2-sentence verdict telling the user which specific sections of the source are worth reading deeply versus skipping.
   - **Candidate Stones (`[1]`–`[5]`):** Present **3 to 5 numbered Candidate Stones** (`[1]`, `[2]`, ...), each prefixed with a relation tag:
     - `[NEW MECHANISM]` — introduces an orthogonal mechanism not yet in Cairn.
     - `[EXTENDS <id>]` — deepens or connects to an existing stone/monolith `<id>`.
     - `[CHALLENGES <id>]` — contradicts or bounds a claim in `<id>`.
   - Every candidate stone MUST strictly follow the **3-Sentence Grain** (50–120 words, pure prose without titles, frontmatter, or `[[wikilinks]]`):
     1. **Sentence 1 (The Edge / Claim):** State the specific tension, failure mode, or boundary condition.
     2. **Sentence 2 (The Load-Bearing Mechanism):** Explain the exact mechanism, formula, or structural trade-off.
     3. **Sentence 3 (Inline Provenance Anchor):** Include an inline `(Author, Year)` or `(Spec §X.Y)` citation.

4. **Confirm Before Dropping:**
   - Wait for explicit user confirmation on which numbered stones to keep (e.g., `"Drop 1 and 3"`).
   - Pipe the approved stones (separated by standalone `---` lines if dropping a linked sequence) into:
     ```bash
     ./cairn drop --stdin --tether=<id>
     ```
     (omit `--tether` for standalone `[NEW MECHANISM]` stones).
