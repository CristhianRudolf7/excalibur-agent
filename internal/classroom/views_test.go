package classroom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientViewsNeverSerializeAnswerKeys(t *testing.T) {
	q := Quiz{ID: "quiz", Questions: []Question{
		{ID: "single", Kind: KindSingle, Prompt: "Pick", Options: []string{"a", "b"}, Correct: []int{1}, Explanation: "server explanation"},
		{ID: "match", Kind: KindMatch, Prompt: "Match", Left: []string{"a", "b"}, Right: []string{"b", "a"}, Pairs: []int{1, 0}, Explanation: "server explanation"},
	}}
	raw, err := json.Marshal(q.View())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"correct"`, `"pairs"`, `"explanation"`, "server explanation"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("answer key in quiz view: %s", raw)
		}
	}
	view := q.View()
	view.Questions[0].Options[0] = "changed"
	if q.Questions[0].Options[0] != "a" {
		t.Fatal("view aliases server question")
	}
	c := Course{ID: "course", Units: []Unit{{ID: "unit", Topics: []Topic{{ID: "topic", Class: &Class{Content: "Lesson"}}}}}}
	raw, err = json.Marshal(c.View())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"correct"`) || strings.Contains(string(raw), `"pairs"`) {
		t.Fatal(string(raw))
	}
	a := Attempt{ID: "attempt", Answers: []Answer{{QuestionID: "match", Pairs: []int{1, 0}}}, Results: []AnswerResult{{QuestionID: "match", Correct: true, Explanation: "Review the rule"}}}
	raw, err = json.Marshal(a.View())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"answers"`) || strings.Contains(string(raw), `"pairs"`) || !strings.Contains(string(raw), `"correct":true`) || strings.Contains(string(raw), "Review the rule") {
		t.Fatal(string(raw))
	}
}
