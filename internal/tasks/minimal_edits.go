package tasks

// MinimalValueEditsReviewGuidance applies to autonomous and assistant reviews.
// Keep it in compact recovery too; it judges the request, not syntax preferences.
func MinimalValueEditsReviewGuidance() string {
	if !envBool("EXCALIBUR_MINIMAL_VALUE_EDITS", true) {
		return ""
	}
	return "- For replacements or renames, check that only the requested values changed.\n"
}
