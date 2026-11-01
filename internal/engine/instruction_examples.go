package engine

import (
	"regexp"
	"sort"
	"strings"
)

// These bounded literal patterns recover examples, not inferred acceptance
// criteria. They only add advisory checklist text, never a finish refusal.
var functionExampleRe = regexp.MustCompile(`(?m)\b([A-Za-z_]\w*\([^()\r\n]{1,160}\))\s*=\s*("[^"\r\n]{0,160}"|'[^'\r\n]{0,160}'|\[[^\]\r\n]{0,160}\]|[-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?|true|false|null)\b?`)
var proseExampleRe = regexp.MustCompile(`(?im)\bfor example[,: \t]+([^\r\n]{1,160}?)\s+(?:should|must|will)\s+(?:print|output|return|produce)\s+([^\r\n]{1,160})`)
var arrowExampleRe = regexp.MustCompile(`(?m)^[ \t]*(?:[Ee]xample:?[ \t]+)?([^\r\n]{1,160}?)[ \t]*(?:→|->|=>)[ \t]*([^\r\n]{1,160})$`)
var fencedExampleRe = regexp.MustCompile("(?ms)^[ \t]*```[^\r\n]*\r?\n(.*?)^[ \t]*```[ \t]*$")
var exampleLabelRe = regexp.MustCompile(`(?im)^[ \t]*(input|expected output|output)[ \t]*:?[ \t]*\r?$`)

type instructionExample struct {
	input, output string
	at            int
}

// instructionExamples reads only the instruction. Up to five literal pairs
// remain in source order, deduplicated; no tool output supplies an example.
func instructionExamples(request string) []string {
	var pairs []instructionExample
	for _, re := range []*regexp.Regexp{functionExampleRe, proseExampleRe, arrowExampleRe} {
		for _, m := range re.FindAllStringSubmatchIndex(request, -1) {
			pairs = append(pairs, instructionExample{request[m[2]:m[3]], request[m[4]:m[5]], m[0]})
		}
	}
	// Support both a single fenced Input/Expected output block and separate
	// labelled fences. Unlabelled code blocks are not input/output examples.
	pending := ""
	for _, m := range fencedExampleRe.FindAllStringSubmatchIndex(request, -1) {
		body := request[m[2]:m[3]]
		labels := exampleLabelRe.FindAllStringSubmatchIndex(body, -1)
		if len(labels) >= 2 && strings.EqualFold(body[labels[0][2]:labels[0][3]], "input") {
			first, second := labels[0], labels[1]
			if !strings.EqualFold(body[second[2]:second[3]], "input") {
				pairs = append(pairs, instructionExample{
					strings.TrimSpace(body[first[1]:second[0]]),
					strings.TrimSpace(body[second[1]:]), m[0],
				})
				pending = ""
			}
			continue
		}
		before := strings.TrimSpace(request[:m[0]])
		line := before[strings.LastIndex(before, "\n")+1:]
		label := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(line, ":")))
		switch label {
		case "input", "example input":
			pending = strings.TrimSpace(body)
		case "output", "expected output", "example output":
			if pending != "" {
				pairs = append(pairs, instructionExample{pending, strings.TrimSpace(body), m[0]})
				pending = ""
			}
		default:
			pending = ""
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].at < pairs[j].at })
	var out []string
	seen := map[string]bool{}
	for _, pair := range pairs {
		in := strings.Join(strings.Fields(strings.Trim(pair.input, "` ")), " ")
		result := strings.Join(strings.Fields(strings.Trim(pair.output, "` .")), " ")
		key := in + "\x00" + result
		if in == "" || result == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, "the instruction gives the example "+in+" → "+result+": show your program reproduces it, and test at least one input not in the examples")
		if len(out) == 5 {
			break
		}
	}
	return out
}
