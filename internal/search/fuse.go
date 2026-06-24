package search

// RRFK is the standard Reciprocal Rank Fusion constant; larger values flatten the
// contribution of top ranks.
const RRFK = 60.0

// ReciprocalRankFusion fuses several ranked lists of opaque keys into a single
// score per key: score(key) = Σ 1/(RRFK + rank), where rank is 0-based within
// each list the key appears in. Keys absent from a list contribute nothing for
// that list. This is robust to the two scans (vector / lexical) using
// incomparable raw scores.
func ReciprocalRankFusion(rankings ...[]string) map[string]float64 {
	scores := make(map[string]float64)
	for _, list := range rankings {
		for rank, key := range list {
			scores[key] += 1.0 / (RRFK + float64(rank))
		}
	}
	return scores
}
