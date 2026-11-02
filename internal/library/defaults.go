package library

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/legacy"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf"
	textreader "github.com/CristhianRudolf7/excalibur-agent/internal/docextract/text"
	officereader "github.com/CristhianRudolf7/excalibur-agent/internal/office/read"
)

// RegisterDefaultExtractors installs native readers and image metadata readers.
// Vision transcription is supplied separately through Options.OCR.
func RegisterDefaultExtractors(reg *docextract.Registry) error {
	if err := reg.Register(pdf.New(pdf.Options{})); err != nil {
		return err
	}
	for _, format := range []string{"xls", "doc", "ppt"} {
		if err := reg.Register(legacy.New(format)); err != nil {
			return err
		}
	}
	for _, format := range []string{"docx", "xlsx", "pptx", "odt", "ods", "odp", "epub"} {
		if err := reg.Register(officereader.New(format)); err != nil {
			return err
		}
	}
	for _, format := range []string{"txt", "md", "html", "csv", "tsv", "json", "png", "jpeg"} {
		if err := reg.Register(textreader.New(format)); err != nil {
			return err
		}
	}
	return nil
}

// PDFPolicy applies the PDF reader's configurable native-text quality threshold.
type PDFPolicy struct{}

func (PDFPolicy) PageNeedsOCR(page docextract.Page) bool { return pdf.PageNeedsOCR(page) }
