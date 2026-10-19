---
name: cairn-sift
description: Audit raw stones in quarry/ against permanent monoliths to sweep out vague pebbles, superseded duplicates, or contradicted claims. Use when the user asks to sift, clean, or audit their Cairn quarry.
---

# Cairn Sift (`cairn-sift`)

Audit raw stones in `quarry/*.md` against permanent syntheses in `monoliths/*.md` to sweep out epistemic rubble while protecting load-bearing stones.

## Workflow

1. **Audit Quarry Against Monoliths:**
   - Read all files in `quarry/*.md`, `monoliths/*.md`, and `trails.tsv`.
   - Leave solid, load-bearing stones untouched and unreported.
   - Classify only problematic stones into three actionable categories:
     - **`Pebble (Erode)`:** Fragmentary reminder, vague platitude, or scratch note lacking a concrete mechanism and lacking an inline `(Author, Year)` citation.
     - **`Superseded (Erode)`:** Raw stone whose mechanism and citations are already fully absorbed into an existing `monoliths/<slug>.md` synthesis.
     - **`Fractured (Flag)`:** Stone whose claim or mechanism is directly contradicted or invalidated by a newer stone or monolith.

2. **Present the Sift Report:**
   - Output a concise Markdown table containing **only** stones in those three categories:
     | Stone ID | Classification | Overlap / Conflict Target | Rationale |
     | :--- | :--- | :--- | :--- |
   - Do not list healthy stones.

3. **Confirm & Erode:**
   - Wait for explicit user approval before modifying files.
   - For approved `Pebble (Erode)` and `Superseded (Erode)` stones, move `quarry/<ULID>.md` to `quarry/.trash/<ULID>.md` (or run `./cairn` erosion), clean up dangling references in `trails.tsv`, and synchronize the index:
     ```bash
     ./cairn index
     ```
