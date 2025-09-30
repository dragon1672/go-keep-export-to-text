// Converts https://takeout.google.com google keep and exports to hopefully useful formats
// This is all in 1 file to make this more copy paste forkable.
package main

import (
	"flag"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/sync/errgroup"

	"github.com/dragon1672/go-keep-export-to-text/keep"
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
	"github.com/dragon1672/go-keep-export-to-text/keep/output/console"
	"github.com/dragon1672/go-keep-export-to-text/keep/output/md"
	"github.com/dragon1672/go-keep-export-to-text/keep/output/opml"
	"github.com/dragon1672/go-keep-export-to-text/keep/output/pdf"
	"github.com/dragon1672/go-keep-export-to-text/keep/output/text"
)

// Inputs
var (
	ZipFilePath   = flag.String("zip_file_path", "example-takeout.zip", "zip file path to be unpacked and parsed")
	SubFolderPath = flag.String("sub_folder_path", "Takeout/Keep/", "required sub folder")
)

// Outputs
var (
	StdOut         = flag.Bool("std_out", false, "optionally print contents to console")
	TxtOutputDir   = flag.String("txt_output_dir", "out", "text file output file dir. Optionally create directories controlled by --create_out")
	MdOutputDir    = flag.String("md_output_dir", "md_out", "markdown output file dir. Optionally create directories controlled by --create_out")
	OutputOPMLFile = flag.String("output_ompl_file", "out.opml", "output OPML file. Optionally create directories controlled by --create_out")

	OutputPDFDir = flag.String("output_pdf_dir", ".", "output PDF file. This will compact multiple notes into a PDF")
	PDFWordLimit = flag.Int("pdf_word_limit", 500000, "Limit the number of words in the PDF output. This is default set to notebooklm limit of 500,000 words")
)

// Configurations
var (
	FileNameStrat      = flag.String("output_file_name_strat", keep.StratDateAndTitle, "How to resolve file names")
	CreateYearFolders  = flag.Bool("output_create_year_folders", true, "Create sub folders for each year")
	CreateMonthFolders = flag.Bool("output_create_month_folders", true, "Create sub folders for each month (requires --output_create_year_folders, otherwise is ignored) This will include both the month number (0 padded), and the month name")
	CreateOut          = flag.Bool("create_out", true, "Attempt to create output dir")
	DefaultTags        = flag.String("default_tags", "google_keep_export", "comma seperated list of default tags to apply to all tags")
)

func loadWriters() []keep.NoteWriter {
	writer := &keep.FileWriter{
		CreateDir: *CreateOut,
		Stdout:    *StdOut,
	}
	fileGenerator := &keep.FileNameGenerator{
		GenerateYearFolders:  *CreateYearFolders,
		GenerateMonthFolders: *CreateMonthFolders,
		NameStrat:            *FileNameStrat,
	}

	var ws []keep.NoteWriter
	if *StdOut {
		ws = append(ws, &console.StdOut{})
	}
	if *OutputOPMLFile != "" {
		ws = append(ws, &opml.Builder{Writer: writer, OutputFile: *OutputOPMLFile})
	}
	if *TxtOutputDir != "" {
		ws = append(ws, &text.Writer{Writer: writer, Generator: fileGenerator, OutDir: *TxtOutputDir})
	}
	if *MdOutputDir != "" {
		ws = append(ws, &md.Writer{Writer: writer, Generator: fileGenerator, OutDir: *MdOutputDir})
	}
	if *OutputPDFDir != "" {
		ws = append(ws, &pdf.Builder{Writer: writer, OutputDir: *OutputPDFDir, WordLimit: *PDFWordLimit})
	}
	return ws
}


func main() {
	flag.Parse()

	reader := loader.ZipToNoteReader{
		SubFolderPath: *SubFolderPath,
		DefaultTags:   strings.Split(*DefaultTags, ","),
	}

	writers := loadWriters()

	g := new(errgroup.Group)
	if err := reader.StreamNotes(*ZipFilePath, func(note *loader.Note) error {
		n := note // local ref

		for _, wc := range writers {
			wc := wc // local ref
			g.Go(func() error {
				return wc.WriteNote(n)
			})
		}

		return nil
	}); err != nil {
		glog.Fatalf("error reading notes from zip file %s: %v", *ZipFilePath, err)
	}
	if err := g.Wait(); err != nil {
		glog.Errorf("error writing notes: %v", err)
	}

	for _, wc := range writers {
		if err := wc.Flush(); err != nil {
			glog.Errorf("error flushing writer: %v", err)
		}
	}
}
