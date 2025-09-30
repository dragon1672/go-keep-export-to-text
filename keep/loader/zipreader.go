package loader

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"archive/zip"
	"encoding/json"
	"path/filepath"

	"github.com/golang/glog"
)

type ZipToNoteReader struct {
	SubFolderPath string
	DefaultTags   []string
}

func (z *ZipToNoteReader) streamZipFiles(source string, fun func(*zip.File) error) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer reader.Close()

	// Do a check for zip slip https://snyk.io/research/zip-slip-vulnerability
	zipSlipCheck, err := filepath.Abs(".")
	if err != nil {
		return err
	}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue // skip directories
		}
		filePath := filepath.Join(zipSlipCheck, f.Name)
		if !strings.HasPrefix(filePath, filepath.Clean(zipSlipCheck)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", filePath)
		}
		if err := fun(f); err != nil {
			return err
		}
	}
	return nil
}
func (z *ZipToNoteReader) file2Note(f *zip.File) (*Note, error) {
	zippedFile, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer zippedFile.Close()

	data, err := io.ReadAll(zippedFile)
	if err != nil {
		return nil, err
	}
	note := &Note{
		FileName: strings.TrimSuffix(filepath.Base(f.FileInfo().Name()), filepath.Ext(f.FileInfo().Name())),
	}
	if err := json.Unmarshal(data, note); err != nil {
		return nil, err
	}
	note.ExtractedTitle = note.Title // keep the original title
	if note.Title == "" {
		glog.Infof("providing default title for file %v", f.FileInfo().Name())
		note.Title = f.FileInfo().Name()
	}
	for _, defaultTag := range z.DefaultTags {
		note.Labels = append(note.Labels, ListLabel{defaultTag})
	}
	return note, nil
}
func (z *ZipToNoteReader) StreamNotes(source string, fun func(*Note) error) error {
	return z.streamZipFiles(source, func(file *zip.File) error {
		if path.Ext(file.Name) != ".json" {
			return nil // skip
		}
		if len(z.SubFolderPath) > 0 && !strings.Contains(file.Name, z.SubFolderPath) {
			return nil // skip
		}
		note, err := z.file2Note(file)
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", file.Name, err)
		}
		if note.IsTrashed || note.IsArchived {
			glog.Infof("skipping trashed or archived entry %v", file.Name)
			return nil // skip the dead stuffs
		}
		return fun(note)
	})
}