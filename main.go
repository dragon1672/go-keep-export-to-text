/* Converts https://takeout.google.com google keep export into text files

1. Export data to https://takeout.google.com (pro-tip: uncheck all and only export google keep)
2.

*/
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
)

var (
	ZipFilePath   = flag.String("zip_file_path", "takeout-example.zip", "zip file path to be unpacked and parsed")
	SubFolderPath = flag.String("sub_folder_path", "Takeout/Keep/", "required sub folder")
	OutputDir     = flag.String("output_dir", "out", "output file dir. Optionally create controlled by --create_out_dir")
	CreateOutDir  = flag.Bool("create_out_dir", true, "Attempt to create output dir")
)

type Note struct {
	Title       string      `json:"title"`
	TextContent string      `json:"textContent"`
	IsTrashed   bool        `json:"isTrashed"`
	IsArchived  bool        `json:"isArchived"`
	ListContent []ListItem  `json:"listContent"`
	Labels      []ListLabel `json:"labels"`
}

type ListItem struct {
	Text      string `json:"text"`
	IsChecked bool   `json:"isChecked"`
}

//"labels":[{"name":"Thoughts"}]
type ListLabel struct {
	Name string `json:"name"`
}

func (n *Note) String() string {
	sb := strings.Builder{}
	sb.WriteString(n.Title)
	sb.WriteRune('\n')
	sb.WriteRune('\n')
	sb.WriteString(n.TextContent)
	listDelim := ""
	for _, listEntry := range n.ListContent {
		sb.WriteString(listDelim)
		sb.WriteRune('[')
		if listEntry.IsChecked {
			sb.WriteRune('X')
		} else {
			sb.WriteRune(' ')
		}
		sb.WriteRune(']')
		sb.WriteRune(' ')
		sb.WriteString(listEntry.Text)
		listDelim = "\n"
	}

	if len(n.Labels) > 0 {
		sb.WriteRune('\n')
		sb.WriteRune('\n')
		labelDelim := ""
		for _, label := range n.Labels {
			sb.WriteString(labelDelim)
			sb.WriteRune('#')
			sb.WriteString(label.Name)
			labelDelim = "\n"
		}
	}
	return sb.String()
}

// processZipSource iterates over zip files.
func processZipSource(source string, fun func(*zip.File) error) error {
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

func writeNoteToFile(note *Note, destination string, createDir bool) error {
	if createDir {
		if err := os.MkdirAll(filepath.Dir(destination), os.ModePerm); err != nil {
			return err
		}
	}

	destinationFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	if _, err := destinationFile.WriteString(note.String()); err != nil {
		return err
	}
	return destinationFile.Sync()
}

func file2Note(f *zip.File) (*Note, error) {
	zippedFile, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer zippedFile.Close()

	data, err := ioutil.ReadAll(zippedFile)
	if err != nil {
		return nil, err
	}
	note := &Note{}
	if err := json.Unmarshal(data, note); err != nil {
		return nil, err
	}
	if note.Title == "" {
		glog.Infof("providing default title for file %v", f.FileInfo().Name())
		note.Title = f.FileInfo().Name()
	}
	return note, nil
}

func main() {
	flag.Parse()
	if err := processZipSource(*ZipFilePath, func(file *zip.File) error {
		if path.Ext(file.Name) != ".json" {
			return nil // skip
		}
		if !strings.Contains(file.Name, *SubFolderPath) {
			return nil // skip
		}
		note, err := file2Note(file)
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", file.Name, err)
		}
		if note.IsTrashed || note.IsArchived {
			glog.Infof("skipping trashed or archived entry %v", file.Name)
			return nil // skip the dead stuffs
		}
		trimmedName := strings.TrimSuffix(filepath.Base(file.FileInfo().Name()), filepath.Ext(file.FileInfo().Name()))
		filePath, err := filepath.Abs(filepath.Join(*OutputDir, trimmedName+".txt"))
		if err != nil {
			return err
		}
		if err := writeNoteToFile(note, filePath, *CreateOutDir); err != nil {
			return fmt.Errorf("error writing file %s: %v", file.Name, err)
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}
