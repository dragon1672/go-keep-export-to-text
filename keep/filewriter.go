package keep

import (
	"fmt"
	"os"
	"path"
	"sync"

	"path/filepath"

	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
)

const (
	StratDirectExport = "direct_export"
	StratFavorDate    = "favor_date"     // attempt to only include the date (YYYY-MM-DD) will fall back to date prefixed `YYYY-MM-DD_${direct_export}`
	StratDateAndTitle = "date_and_title" // attempt to set (YYYY-MM-DD-TITLE) will default to (YYYY-MM-DD) if no clear title, and will fall back to date prefixed `YYYY-MM-DD_${direct_export}`
)

type FileWriter struct {
	CreateDir bool
	Stdout    bool // also write to std out
}

func (f *FileWriter) DirPrep(destination string) error {
	if f.CreateDir {
		if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}

func (f *FileWriter) WriteFile(data string, destination string) error {
	if f.CreateDir {
		if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
			return err
		}
	}
	if f.Stdout {
		fmt.Printf("```%s\n%s\n```\n", destination, data)
	}

	destinationFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	if _, err := destinationFile.WriteString(data); err != nil {
		return err
	}
	return destinationFile.Sync()
}

type FileNameGenerator struct {
	GenerateYearFolders  bool
	GenerateMonthFolders bool
	NameStrat            string

	mu            sync.RWMutex
	reservedPaths map[string]bool
}

type NoteWriteRequest struct {
	note *loader.Note
	err  chan error
}
func (f *FileNameGenerator) GenerateAndReserve(n *loader.Note) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reservedPaths == nil {
		f.reservedPaths = make(map[string]bool)
	}
	fileName := n.FileName // default to STRAT_DIRECT_EXPORT
	switch f.NameStrat {
	case StratDirectExport:
		fileName = n.FileName
	case StratFavorDate:
		fileName = n.CreatedMicros.String() // attempt to make just the date
		if _, ok := f.reservedPaths[fileName]; ok {
			fileName = fmt.Sprintf("%s_%s", n.CreatedMicros.String(), n.FileName)
		}
	case StratDateAndTitle:
		fileName = n.CreatedMicros.String() // attempt to make just the date
		if n.ExtractedTitle != "" {
			fileName = fmt.Sprintf("%s_%s", fileName, n.ExtractedTitle)
		}
		if _, ok := f.reservedPaths[fileName]; ok {
			fileName = fmt.Sprintf("%s_%s", n.CreatedMicros.String(), n.FileName)
		}
	}
	if f.GenerateYearFolders {
		prefix := fmt.Sprint(n.CreatedMicros.Time().Year())
		if f.GenerateMonthFolders {
			month := fmt.Sprintf("%02d-%s", n.CreatedMicros.Time().Month(), n.CreatedMicros.Time().Month())
			prefix = path.Join(prefix, month)
		}
		fileName = path.Join(prefix, fileName)
	}
	f.reservedPaths[fileName] = true
	return fileName
}