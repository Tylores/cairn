---
name: cairn-link
description: Discover non-obvious conceptual connections across Cairn stones and monoliths and append links to trails.tsv. Use when the user asks to link notes, update trails, or connect stones in Cairn.
---

# Cairn Link (`cairn-link`)

Discover non-obvious, high-signal conceptual trails across Cairn stones and monoliths and record them in `trails.tsv`.

## Workflow

1. **Survey the Knowledge Base:**
   - Read `trails.tsv`, `quarry/*.md`, and `monoliths/*.md` (`monolith:<slug>`).

2. **Identify High-Value Conceptual Connections:**
   - Look for complementary mechanisms, shared architectural constraints, or cause-and-effect pairs that use different surface vocabulary (where lexical search alone might miss the bridge).
   - Avoid linking notes just because they share a generic topic word.
   - **Keep the Graph Sparse:** Maintain at most 1–3 strong trails per stone so `Trail Neighbor` expansion in the Horizon remains high-signal.

3. **Propose & Apply Trails:**
   - Present a concise Markdown table (`Source ID` | `Target ID` | `Why This Bridge Matters`).
   - Upon approval (or when instructed to apply directly), append `source_id\ttarget_id` lines (tab-separated) to `trails.tsv`, avoiding duplicate edges, and synchronize the SQLite index:
     ```bash
     ./cairn index
     ```
