package engine

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"cairn/internal/store"
)

var tokenRegex = regexp.MustCompile(`[A-Za-z0-9]+`)

// ScoredHit represents a ranked note surfaced in the Horizon discovery pane or CLI query.
type ScoredHit struct {
	Note     store.Note `json:"note"`
	Category string     `json:"category"`
	Score    float64    `json:"score"`
}

// Engine executes Cairn's deterministic retrieval and epistemic weathering pipeline.
type Engine struct {
	Store *store.Store
}

// New creates a new retrieval Engine backed by st.
func New(st *store.Store) *Engine {
	return &Engine{Store: st}
}

// ApplyWeathering computes the three-tier epistemic weathering score:
//
//	S_weathered = S_raw * w_tier * exp(-lambda * months_old)
//
// - Carved Monolith (IsMonolith == true): w_tier = 1.20, lambda = 0.00
// - Anchored Stone (HasCitation || HasTrail): w_tier = 1.00, lambda = 0.03
// - Unanchored Raw Stone: w_tier = 1.00, lambda = 0.20
func ApplyWeathering(rawScore float64, note store.Note, now time.Time) float64 {
	var wTier, lambda float64
	switch {
	case note.IsMonolith:
		wTier = 1.20
		lambda = 0.00
	case note.HasCitation || note.HasTrail || store.HasCitation(note.Body):
		wTier = 1.00
		lambda = 0.03
	default:
		wTier = 1.00
		lambda = 0.20
	}

	monthsOld := 0.0
	if note.CreatedAt > 0 {
		ageHours := now.Sub(time.Unix(note.CreatedAt, 0)).Hours()
		if ageHours > 0 {
			monthsOld = ageHours / (24.0 * 30.4375)
		}
	}

	return rawScore * wTier * math.Exp(-lambda*monthsOld)
}

// SanitizeFTSQuery extracts alphanumeric tokens (>= 2 chars) and joins them with OR for FTS5.
func SanitizeFTSQuery(raw string) string {
	matches := tokenRegex.FindAllString(raw, -1)
	seen := make(map[string]bool, len(matches))
	tokens := make([]string, 0, len(matches))
	for _, m := range matches {
		lower := strings.ToLower(m)
		if len(lower) < 2 || isStopword(lower) || seen[lower] {
			continue
		}
		seen[lower] = true
		tokens = append(tokens, `"`+lower+`"`)
	}
	return strings.Join(tokens, " OR ")
}

func isStopword(tok string) bool {
	switch tok {
	case "an", "and", "are", "as", "at", "be", "by", "for", "from", "in", "into",
		"is", "it", "of", "on", "or", "that", "the", "to", "with":
		return true
	default:
		return false
	}
}

// TokenSet extracts lowercased alphanumeric tokens strictly longer than minLen characters.
func TokenSet(text string, minLen int) map[string]struct{} {
	matches := tokenRegex.FindAllString(strings.ToLower(text), -1)
	set := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if len(m) > minLen && !isStopword(m) {
			set[m] = struct{}{}
		}
	}
	return set
}

// JaccardSimilarity computes |A ∩ B| / |A ∪ B| between two token sets.
func JaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union <= 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// ComputeBridgeScore calculates the penalized harmonic mean of token overlap between
// a candidate token set and two anchor token sets tokA and tokB:
//
//	BridgeScore = (2 * simA * simB) / (simA + simB) - 0.25 * |simA - simB|
//
// Both simA and simB must fall within [0.06, 0.75].
func ComputeBridgeScore(cand, tokA, tokB map[string]struct{}) (float64, bool) {
	simA := JaccardSimilarity(cand, tokA)
	simB := JaccardSimilarity(cand, tokB)
	if simA < 0.06 || simA > 0.75 || simB < 0.06 || simB > 0.75 {
		return 0, false
	}
	if simA+simB == 0 {
		return 0, false
	}
	harmonic := (2.0 * simA * simB) / (simA + simB)
	score := harmonic - 0.25*math.Abs(simA-simB)
	if score <= 0 {
		return 0, false
	}
	return score, true
}

// QueryHorizon runs the 4-stage deterministic retrieval pipeline with MMR diversity re-ranking.
func (e *Engine) QueryHorizon(queryText string, anchorIDs []string, limit int) []ScoredHit {
	if limit <= 0 {
		limit = 8
	}
	now := time.Now().UTC()

	allNotes, err := e.Store.ListNotes()
	if err != nil || len(allNotes) == 0 {
		return nil
	}

	notesByID := make(map[string]store.Note, len(allNotes))
	for _, n := range allNotes {
		notesByID[n.ID] = n
	}

	excludedAnchors := make(map[string]bool, len(anchorIDs))
	cleanAnchors := make([]string, 0, len(anchorIDs))
	for _, id := range anchorIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			excludedAnchors[id] = true
			cleanAnchors = append(cleanAnchors, id)
		}
	}

	candidates := make(map[string]ScoredHit)
	upsertCandidate := func(note store.Note, category string, weatheredScore float64) {
		if excludedAnchors[note.ID] {
			return
		}
		if prev, exists := candidates[note.ID]; !exists || weatheredScore > prev.Score {
			candidates[note.ID] = ScoredHit{
				Note:     note,
				Category: category,
				Score:    weatheredScore,
			}
		}
	}

	// Category 1 — Trail Neighbor of active anchorIDs (S_raw = 1.10)
	if len(cleanAnchors) > 0 {
		if neighbors, err := e.Store.GetTrailNeighbors(cleanAnchors); err == nil {
			for _, nbID := range neighbors {
				if note, ok := notesByID[nbID]; ok {
					upsertCandidate(note, "Trail Neighbor", ApplyWeathering(1.10, note, now))
				}
			}
		}
	}

	// Category 2 — Direct Match / Monolith (FTS5 BM25) + 1-hop Trail Neighbors of top 3 FTS hits
	ftsExpr := SanitizeFTSQuery(queryText)
	if ftsExpr != "" {
		if ftsHits, err := e.Store.SearchFTS(ftsExpr, 30); err == nil {
			topFTSIDs := make([]string, 0, 3)
			for rank, note := range ftsHits {
				rawScore := 60.0 / float64(60+rank+1)
				cat := "Direct Match"
				if note.IsMonolith {
					cat = "Monolith"
				}
				upsertCandidate(note, cat, ApplyWeathering(rawScore, note, now))
				if len(topFTSIDs) < 3 {
					topFTSIDs = append(topFTSIDs, note.ID)
				}
			}

			if len(topFTSIDs) > 0 {
				if ftsNeighbors, err := e.Store.GetTrailNeighbors(topFTSIDs); err == nil {
					for _, nbID := range ftsNeighbors {
						if note, ok := notesByID[nbID]; ok {
							upsertCandidate(note, "Trail Neighbor", ApplyWeathering(0.85, note, now))
						}
					}
				}
			}
		}
	}

	// Category 3 — Stepping Stone (BridgeScore between first and last anchor)
	if len(cleanAnchors) >= 2 {
		firstNote, okA := notesByID[cleanAnchors[0]]
		lastNote, okB := notesByID[cleanAnchors[len(cleanAnchors)-1]]
		if okA && okB {
			tokA := TokenSet(firstNote.Body, 3)
			tokB := TokenSet(lastNote.Body, 3)
			for _, note := range allNotes {
				if excludedAnchors[note.ID] {
					continue
				}
				candTok := TokenSet(note.Body, 3)
				if bridge, ok := ComputeBridgeScore(candTok, tokA, tokB); ok {
					upsertCandidate(note, "Stepping Stone", ApplyWeathering(bridge*2.5, note, now))
				}
			}
		}
	}

	primaryPool := make([]ScoredHit, 0, len(candidates))
	for _, hit := range candidates {
		primaryPool = append(primaryPool, hit)
	}
	selected := selectMMR(primaryPool, limit, 0.65)

	// Category 4 — Fallback (Deep Quarry / Monolith) if total hits < limit
	if len(selected) < limit {
		alreadyPicked := make(map[string]bool, len(selected)+len(excludedAnchors))
		for id := range excludedAnchors {
			alreadyPicked[id] = true
		}
		for _, s := range selected {
			alreadyPicked[s.Note.ID] = true
		}

		var fallbackPool []ScoredHit
		for _, note := range allNotes {
			if alreadyPicked[note.ID] {
				continue
			}
			cat := "Deep Quarry"
			if note.IsMonolith {
				cat = "Monolith"
			}
			fallbackPool = append(fallbackPool, ScoredHit{
				Note:     note,
				Category: cat,
				Score:    ApplyWeathering(0.40, note, now),
			})
		}

		needed := limit - len(selected)
		selected = append(selected, selectMMR(fallbackPool, needed, 0.65)...)
	}

	return selected
}

// selectMMR re-ranks up to the top 30 candidates using Maximal Marginal Relevance (lambda = 0.65).
func selectMMR(pool []ScoredHit, k int, lambda float64) []ScoredHit {
	if len(pool) == 0 || k <= 0 {
		return nil
	}

	sorted := make([]ScoredHit, len(pool))
	copy(sorted, pool)
	sort.Slice(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Score-sorted[j].Score) > 1e-9 {
			return sorted[i].Score > sorted[j].Score
		}
		return sorted[i].Note.ID < sorted[j].Note.ID
	})

	if len(sorted) > 30 {
		sorted = sorted[:30]
	}

	tokenCache := make(map[string]map[string]struct{}, len(sorted))
	for _, h := range sorted {
		tokenCache[h.Note.ID] = TokenSet(h.Note.Body, 3)
	}

	selected := make([]ScoredHit, 0, minInt(k, len(sorted)))
	used := make([]bool, len(sorted))

	// Always pick the highest-scored item first
	selected = append(selected, sorted[0])
	used[0] = true

	for len(selected) < k && len(selected) < len(sorted) {
		bestIdx := -1
		bestMMR := -math.MaxFloat64

		for i, cand := range sorted {
			if used[i] {
				continue
			}
			maxSim := 0.0
			candTokens := tokenCache[cand.Note.ID]
			for _, sel := range selected {
				sim := JaccardSimilarity(candTokens, tokenCache[sel.Note.ID])
				if sim > maxSim {
					maxSim = sim
				}
			}
			mmrScore := lambda*cand.Score - (1.0-lambda)*maxSim
			if mmrScore > bestMMR {
				bestMMR = mmrScore
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, sorted[bestIdx])
	}

	return selected
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
