# Cairn (`cairn`)

**Cairn** is a local-first, zero-taxonomy Unix CLI and 3-pane Bubble Tea TUI note-taking workbench written in pure Go (`CGO_ENABLED=0`).

It separates raw atomic capture (**The Quarry**) from permanent human-named syntheses (**The Monoliths**), uses a disposable derived SQLite + FTS5 index (`quarry.db`), continuously emits an active working context mirror (`.cairn/trail.md`), and delegates semantic operations to four portable CLI Agent Skills under `.agents/skills/`.

---

## 1. Core Architectural Principles

1. **Zero Embedded AI / Zero Network Calls:**
   The compiled Go binary (`cairn`) contains zero HTTP/LLM client code and zero CGo dependencies. All semantic linking, source skimming, monolith synthesis, and rubble sifting live in transparent, editable Markdown skills under `.agents/skills/`.
2. **Two-Tier Epistemic Storage:**
   - **The Quarry (`quarry/<ULID>.md`):** Flat directory of raw atomic stones (50–120 words). Pure prose—no YAML frontmatter, no titles, no `[[wikilinks]]`, and no `#tags`.
   - **The Monoliths (`monoliths/<slug>.md`):** Permanent, human-named architectural syntheses carved only after a stack of stones has solved a real problem.
3. **Sparse Directed Graph (`trails.tsv`):**
   Explicit connections between stones and monoliths are stored out-of-band in a Git-tracked tab-separated file (`source_id\ttarget_id`), keeping Markdown files clean.
4. **Workbench Over Library (`.cairn/trail.md`):**
   Every stack mutation in the 3-pane TUI (`Horizon` | `Slate` | `Cairn`) automatically compiles the active session into `.cairn/trail.md` with invisible `<!-- stone:<ID> -->` provenance comments so terminal coding agents can reference `@.cairn/trail.md` directly.
5. **Disposable Derived Index (`quarry.db`):**
   SQLite (WAL mode + FTS5 `porter unicode61`) indexes `quarry/*.md`, `monoliths/*.md`, and `trails.tsv`. Deleting `quarry.db` loses zero permanent knowledge (`cairn index --rebuild`).

---

## 2. Repository & Directory Resolution

`DefaultRootDir()` resolves the Cairn workspace root in the following order:
1. `$CAIRN_DIR` environment variable if set.
2. Current working directory (`.`) if both `./quarry` and `./monoliths` exist locally.
3. Fallback to `~/.local/share/cairn`.

```text
.
├── .gitignore
├── README.md
├── go.mod
├── cmd/cairn/main.go
├── internal/
│   ├── store/store.go
│   ├── engine/engine.go
│   └── tui/workbench.go
├── .agents/skills/
│   ├── cairn-rockhound/SKILL.md
│   ├── cairn-link/SKILL.md
│   ├── cairn-carve/SKILL.md
│   └── cairn-sift/SKILL.md
├── monoliths/*.md
├── quarry/*.md
├── trails.tsv
└── .cairn/trail.md
```

---

## 3. Rockhound's 3-Sentence Grain Guide

Raw stones in `quarry/<ULID>.md` follow the **3-Sentence Grain** (50–120 words of dense, standalone prose):

1. **Sentence 1 — The Edge / Claim:** State the specific problem, failure mode, or counter-intuitive boundary condition (never a generic textbook definition).
2. **Sentence 2 — The Load-Bearing Mechanism:** Explain the exact causal mechanism, equation, data structure, or architectural trade-off that resolves the tension.
3. **Sentence 3 — Inline Provenance Anchor:** Anchor the claim with an inline `(Author, Year)` citation or specification section `(Spec §X.Y)` so SQLite FTS5 BM25 clusters co-cited notes naturally via high-IDF surname and year tokens.

---

## 4. Deterministic Retrieval & Epistemic Weathering Engine

### 4.1 Three-Tier Exponential Weathering
Every raw retrieval score $S_{\text{raw}}$ is multiplied by a tier authority weight and exponential age decay:

$$S_{\text{weathered}} = S_{\text{raw}} \cdot w_{\text{tier}} \cdot e^{-\lambda \cdot \text{months\_old}}$$

| Epistemic Tier | Condition | Authority ($w_{\text{tier}}$) | Decay ($\lambda$) | Half-Life |
| :--- | :--- | :---: | :---: | :---: |
| **Carved Monolith** | `IsMonolith == true` | `1.20` | `0.00` | Immortal ($\infty$) |
| **Anchored Stone** | `HasCitation` or `HasTrail` | `1.00` | `0.03` | ~23 months |
| **Unanchored Raw Stone** | Neither cited nor linked | `1.00` | `0.20` | ~3.5 months |

### 4.2 Four-Stage Horizon Pipeline + MMR Diversity
1. **Category 1 — `Trail Neighbor`:** 1-hop neighbors of active `anchorIDs` in `trails.tsv` ($S_{\text{raw}} = 1.10$).
2. **Category 2 — `Direct Match` / `Monolith`:** FTS5 BM25 query over sanitized tokens ($\ge 2$ chars) joined with `OR`, scored as $S_{\text{raw}} = \frac{60.0}{60 + \text{rank} + 1}$. The top 3 FTS hits also expand their 1-hop `Trail Neighbors` at $S_{\text{raw}} = 0.85$.
3. **Category 3 — `Stepping Stone` (`BridgeScore`):** When $\ge 2$ anchors are active, computes token Jaccard similarities $\text{sim}_A, \text{sim}_B \in [0.06, 0.75]$ against the first and last anchors:
   $$\text{BridgeScore}(c, A, B) = \frac{2 \cdot \text{sim}_A \cdot \text{sim}_B}{\text{sim}_A + \text{sim}_B} - 0.25 \cdot |\text{sim}_A - \text{sim}_B|$$
   Weathered with $S_{\text{raw}} = 2.5 \cdot \text{BridgeScore}$.
4. **Category 4 — `Deep Quarry` / `Monolith` Fallback:** Backfills remaining slots with $S_{\text{raw}} = 0.40$ weathered by age.
5. **MMR Re-ranking (`selectMMR`):** Greedily selects diverse hits with $\lambda = 0.65$ using token Jaccard similarity.

---

## 5. CLI Command Surface

```bash
# Build pure-Go binary
CGO_ENABLED=0 go build -o cairn ./cmd/cairn

# Launch 3-pane Bubble Tea workbench
./cairn [trail-name]

# Drop one or more stones (splits on standalone --- lines)
./cairn drop [--stdin] [--tether=<id>]

# Query the Horizon from the CLI
./cairn query "<text>" [--limit=8] [--json]

# Compile active session into .cairn/trail.md and print to stdout
./cairn blaze [trail-name]

# Synchronize or rebuild SQLite FTS5 index from disk
./cairn index [--rebuild]
```

---

## 6. 3-Pane TUI Keybindings

| Key | Action |
| :--- | :--- |
| `1`, `2`, `3` | Focus `1. HORIZON`, `2. SLATE`, or `3. CAIRN` pane |
| `j` / `k` (`↓` / `↑`) | Move cursor in `Horizon` or `Cairn` and update `Slate` preview |
| `l` / `→` | Walk trail into selected stone (pushes onto `breadcrumbs`) |
| `h` / `←` | Pop breadcrumb and step backward |
| `Space` | Push active note onto `Cairn` stack and write `.cairn/trail.md` |
| `t` | Tether active note to the last stacked `Cairn` stone in `trails.tsv` |
| `J` / `K` | Reorder selected item down / up in `Cairn` stack |
| `s` | Prompt for virtual section header (`## <Title>`) in `Cairn` stack |
| `x` | Remove item from `Cairn` stack, or erode raw stone in `Horizon` to `quarry/.trash/` |
| `i` or `c` | Focus `Chisel` scratchpad (`Esc` blurs & saves; typing triggers 250ms debounced query) |
| `/` | Focus search filter (`Enter` filters Horizon; `Esc` cancels) |
| `Tab` | Toggle `Slate` between Single-Stone View and Cairn Accordion View |
| `e` | Edit active stone or monolith in `$EDITOR` (`tea.ExecProcess`) and re-index |
| `n` | Drop a new stone in `$EDITOR` (`tea.ExecProcess`), cleave, and re-index |
| `Enter` (in `Cairn`) | Write `.cairn/trail.md` and open it in `$EDITOR` |
| `q` / `Ctrl+C` | Save session, write `.cairn/trail.md`, and quit |

---

## 7. Portable Agent Skills (`.agents/skills/`)

- **`cairn-rockhound`**: Skim a paper, specification, or URL to pan for 3–5 high-signal candidate Stones in the 3-Sentence Grain calibrated against existing Monoliths and `.cairn/trail.md`.
- **`cairn-link`**: Discover non-obvious conceptual connections across `quarry/*.md` and `monoliths/*.md` and append sparse links to `trails.tsv`.
- **`cairn-carve`**: Distill `.cairn/trail.md` into a permanent Monolith in `monoliths/<slug>.md` and move consumed raw stones to `quarry/.trash/`.
- **`cairn-sift`**: Audit `quarry/*.md` against `monoliths/*.md` to classify `Pebble (Erode)`, `Superseded (Erode)`, and `Fractured (Flag)` stones.
