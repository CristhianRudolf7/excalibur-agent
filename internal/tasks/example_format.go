package tasks

// ExampleFormatReviewGuidance preserves the literal rule when example values
// cannot be reproduced, for assistant workers and autonomous runs alike.
func ExampleFormatReviewGuidance() string {
	if !envBool("EXCALIBUR_EXAMPLE_FORMAT", true) {
		return ""
	}
	return "- If request example values cannot be reproduced from supplied input, treat them as format-only; invent no offsets, scaling or renaming to match. For programs meant for other inputs, require an executed test on a second, different input created by the executor.\n"
}
