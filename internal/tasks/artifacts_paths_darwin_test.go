//go:build darwin

package tasks

import "testing"

func TestDarwinTemporaryDeliverableRoots(t *testing.T) {
	for _, path := range []string{
		"/var/folders/ab/user/T/task/result.csv",
		"/private/var/folders/ab/user/T/task/result.csv",
		"/private/tmp/task/result.csv",
	} {
		reqs := ExtractArtifactRequirements("Write the report to `"+path+"`", 8)
		found := false
		for _, req := range reqs {
			found = found || req.Path == path && HardArtifactRequirement(req)
		}
		if !found {
			t.Errorf("temporary output not required: %q: %+v", path, reqs)
		}
	}
	for _, path := range []string{"/var/db/system.db", "/private/var/db/system.db", "/var/folders-sibling/result.csv"} {
		if got := StrictAbsolutePaths(path, ""); len(got) != 0 {
			t.Errorf("system or sibling path admitted: %v", got)
		}
	}
}
