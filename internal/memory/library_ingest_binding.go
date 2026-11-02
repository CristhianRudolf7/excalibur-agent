package memory

import "context"

// LibrarySubmitter keeps memory tools independent of the background worker package.
type LibrarySubmitter interface {
	SubmitLibrary(context.Context, string, DocMeta, string) (DocumentInfo, bool, error)
}
type librarySubmitterBinding struct{ submitter LibrarySubmitter }

// BindLibraryIngester attaches a daemon-owned worker without an import cycle.
func (s *Service) BindLibraryIngester(submitter LibrarySubmitter) {
	s.librarySubmitter.Store(&librarySubmitterBinding{submitter: submitter})
}
