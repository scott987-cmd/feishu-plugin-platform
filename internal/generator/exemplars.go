package generator

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Few-shot grounding: the catalog's verified plugins (NL request → exact DSL we
// want emitted) are embedded and the 2-3 most relevant are injected into the
// generation prompt. Grounding on known-good output is the cheapest lever on
// first-pass success, and every exemplar is Validate()-checked by a unit test so
// it can never drift into an invalid example.

//go:embed exemplars/field.json
var fieldExemplarsRaw []byte

//go:embed exemplars/action.json
var actionExemplarsRaw []byte

// exemplar is one verified NL→DSL pair. DSL is kept as raw JSON so it is injected
// verbatim (and so the validation test can unmarshal it into the real type).
type exemplar struct {
	NL  string          `json:"nl"`
	DSL json.RawMessage `json:"dsl"`
}

var (
	fieldExemplars  = mustParseExemplars(fieldExemplarsRaw)
	actionExemplars = mustParseExemplars(actionExemplarsRaw)
)

func mustParseExemplars(raw []byte) []exemplar {
	var ex []exemplar
	if err := json.Unmarshal(raw, &ex); err != nil {
		panic("generator: bad embedded exemplars: " + err.Error())
	}
	return ex
}

var asciiWordRe = regexp.MustCompile(`[a-z0-9]+`)

// promptTokens lowers s into a bag of features for cheap lexical overlap, with no
// embeddings or external deps: ascii words (len≥2) plus CJK character bigrams
// (Chinese has no spaces, so adjacent-char bigrams approximate terms).
func promptTokens(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range asciiWordRe.FindAllString(strings.ToLower(s), -1) {
		if len(w) >= 2 {
			set[w] = struct{}{}
		}
	}
	var run []rune
	flush := func() {
		for i := 0; i+1 < len(run); i++ {
			set[string(run[i:i+2])] = struct{}{}
		}
		run = run[:0]
	}
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff { // CJK Unified Ideographs
			run = append(run, r)
		} else {
			flush()
		}
	}
	flush()
	return set
}

// retrieveExemplars returns up to k exemplars from pool ranked by lexical overlap
// with prompt. It always returns min(k, len(pool)) of them: even a weak match
// grounds the model on valid structure, and ties keep the file's (curated) order.
func retrieveExemplars(prompt string, pool []exemplar, k int) []exemplar {
	if k <= 0 || len(pool) == 0 {
		return nil
	}
	pt := promptTokens(prompt)
	type scored struct {
		ex    exemplar
		score int
		idx   int
	}
	ranked := make([]scored, len(pool))
	for i, ex := range pool {
		et := promptTokens(ex.NL)
		n := 0
		for tok := range et {
			if _, ok := pt[tok]; ok {
				n++
			}
		}
		ranked[i] = scored{ex: ex, score: n, idx: i}
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].score != ranked[b].score {
			return ranked[a].score > ranked[b].score
		}
		return ranked[a].idx < ranked[b].idx
	})
	if k > len(ranked) {
		k = len(ranked)
	}
	out := make([]exemplar, k)
	for i := 0; i < k; i++ {
		out[i] = ranked[i].ex
	}
	return out
}

// fewShotBlock formats exemplars as a system-prompt addendum: each is the request
// and the exact (compacted) JSON to emit. Returns "" when there are none.
func fewShotBlock(ex []exemplar) string {
	if len(ex) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nWORKED EXAMPLES — each is a real, verified request and the exact JSON you should emit. Mirror these PATTERNS (expr/template/auth/bodyJson/conditional choices); adapt names and values to the user's actual request, do not copy verbatim:\n")
	for _, e := range ex {
		var compact bytes.Buffer
		if err := json.Compact(&compact, e.DSL); err != nil {
			continue
		}
		fmt.Fprintf(&b, "• Request: %s\n  Emit: %s\n", e.NL, compact.String())
	}
	return b.String()
}
