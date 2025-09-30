package pdf

import (
	"fmt"
	"sync"

	"codeberg.org/go-pdf/fpdf"
	"github.com/golang/glog"

	"github.com/dragon1672/go-keep-export-to-text/keep"
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
	"github.com/dragon1672/go-keep-export-to-text/keep/output/text"
)

// Builder buffers notes in memory then writes to a single file.
type Builder struct {
	mu        sync.RWMutex
	currentPDF	   *fpdf.Fpdf
	currentWordCount int
	outputCnt int

	OutputDir string
	Writer    *keep.FileWriter
	WordLimit int
}

func (b *Builder) WriteNote(note *loader.Note) error {
	title, subheader, body, err := text.Note2TxtParts(note)
	if err != nil {
		return err
	}

	wordCount := keep.CountWords(title,subheader,body)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.currentPDF == nil {
		b.currentPDF = fpdf.New("P", "mm", "A4", "")
	}
	if b.currentWordCount + wordCount > b.WordLimit {
		// Assumption that all notes are similar sizes so no fancy packing algorithm.
		// Just flush when we hit the limit.
		if err := b.unlockedFlush(); err != nil {
			return err
		}
	}

	b.currentPDF.AddPage()
	b.currentPDF.SetFont("Arial", "B", 14)
	b.currentPDF.MultiCell(0, 10, title, "", "C", false)
	b.currentPDF.SetFont("Arial", "I", 10)
	b.currentPDF.MultiCell(0, 5, subheader, "", "C", false)
	b.currentPDF.Ln(5)
	b.currentPDF.SetFont("Arial", "", 12)
	b.currentPDF.MultiCell(0, 5, body, "", "L", false)
	b.currentWordCount += wordCount
	return nil
}

func (b *Builder) unlockedFlush() error {
	outFile := fmt.Sprintf("%s/out_%d.pdf", b.OutputDir, b.outputCnt)
	b.Writer.DirPrep(outFile)
	glog.Infof("flushing %d words to PDF to %s", b.currentWordCount, outFile)
	if err := b.currentPDF.OutputFileAndClose(outFile); err != nil {
		return err
	}
	b.outputCnt++
	b.currentPDF = fpdf.New("P", "mm", "A4", "")
	b.currentWordCount = 0
	return nil
}


func (b *Builder) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.unlockedFlush()
}

var _ keep.NoteWriter = (*Builder)(nil)