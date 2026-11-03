package classroom

import (
	"math/rand/v2"
	"testing"
)

func TestShuffleKeepsAnswersAndMovesThem(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	firstCorrect := 0
	for i := 0; i < 200; i++ {
		q := Question{Kind: KindSingle, Options: []string{"right", "a", "b", "c"}, Correct: []int{0}}
		shuffleQuestion(&q, r)
		if q.Options[q.Correct[0]] != "right" {
			t.Fatalf("correct option lost: %+v", q)
		}
		if q.Correct[0] == 0 {
			firstCorrect++
		}
		m := Question{Kind: KindMultiple, Options: []string{"r1", "r2", "x", "y"}, Correct: []int{0, 1}}
		shuffleQuestion(&m, r)
		got := map[string]bool{m.Options[m.Correct[0]]: true, m.Options[m.Correct[1]]: true}
		if !got["r1"] || !got["r2"] {
			t.Fatalf("multiple answers lost: %+v", m)
		}
		p := Question{Kind: KindMatch, Left: []string{"a", "b", "c"}, Right: []string{"A", "B", "C"}, Pairs: []int{0, 1, 2}}
		shuffleQuestion(&p, r)
		for i, l := range p.Left {
			if p.Right[p.Pairs[i]] != map[string]string{"a": "A", "b": "B", "c": "C"}[l] {
				t.Fatalf("pairs lost: %+v", p)
			}
		}
	}
	if firstCorrect > 100 {
		t.Fatalf("correct option stays first too often: %d/200", firstCorrect)
	}
}
