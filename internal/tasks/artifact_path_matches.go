package tasks

func absolutePaths(text string) []string {
	var paths []string
	for _, loc := range absolutePathIndices(text) {
		paths = append(paths, text[loc[0]:loc[1]])
	}
	return paths
}
