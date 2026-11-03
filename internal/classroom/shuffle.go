package classroom

import "math/rand/v2"

// shuffleQuestion randomizes the presentation order of a validated question
// so the position of the correct option carries no information. Choice
// questions permute their options and remap Correct; matching questions
// permute the right column and remap Pairs. The left column keeps its order.
func shuffleQuestion(q *Question, r *rand.Rand) {
	switch q.Kind {
	case KindSingle, KindMultiple:
		perm := r.Perm(len(q.Options)) // perm[new] = old
		pos := make([]int, len(perm))  // pos[old] = new
		opts := make([]string, len(q.Options))
		for newIdx, oldIdx := range perm {
			opts[newIdx] = q.Options[oldIdx]
			pos[oldIdx] = newIdx
		}
		q.Options = opts
		correct := make([]int, len(q.Correct))
		for i, old := range q.Correct {
			correct[i] = pos[old]
		}
		q.Correct = correct
	case KindMatch:
		perm := r.Perm(len(q.Right))
		pos := make([]int, len(perm))
		right := make([]string, len(q.Right))
		for newIdx, oldIdx := range perm {
			right[newIdx] = q.Right[oldIdx]
			pos[oldIdx] = newIdx
		}
		q.Right = right
		pairs := make([]int, len(q.Pairs))
		for i, old := range q.Pairs {
			pairs[i] = pos[old]
		}
		q.Pairs = pairs
	}
}
