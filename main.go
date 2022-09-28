// Converts https://takeout.google.com google keep and exports to hopefully useful formats
// This is all in 1 file to make this more copy paste forkable.
package main

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/golang/glog"
)

var (
	ZipFilePath    = flag.String("zip_file_path", "takeout-example.zip", "zip file path to be unpacked and parsed")
	SubFolderPath  = flag.String("sub_folder_path", "Takeout/Keep/", "required sub folder")
	StdOut         = flag.Bool("std_out", true, "optionally print contents to console")
	OutputDir      = flag.String("output_dir", "out", "output file dir. Optionally create controlled by --create_out")
	OutputOPMLFile = flag.String("output_ompl_file", "out.opml", "output OPML file. Optionally create controlled by --create_out")
	CreateOut      = flag.Bool("create_out", true, "Attempt to create output dir")
)

type Note struct {
	Title         string      `json:"title"`
	TextContent   string      `json:"textContent"`
	IsTrashed     bool        `json:"isTrashed"`
	IsArchived    bool        `json:"isArchived"`
	ListContent   []ListItem  `json:"listContent"`
	Labels        []ListLabel `json:"labels"`
	EditedMicros  *MicroTime  `json:"userEditedTimestampUsec"`
	CreatedMicros *MicroTime  `json:"createdTimestampUsec"`
}

type ListItem struct {
	Text      string `json:"text"`
	IsChecked bool   `json:"isChecked"`
}

//"labels":[{"name":"Thoughts"}]
type ListLabel struct {
	Name string `json:"name"`
}

type MicroTime time.Time

func (j *MicroTime) UnmarshalJSON(data []byte) error {
	millis, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	*j = MicroTime(time.Unix(0, millis*int64(time.Microsecond)))
	return nil
}

func (j *MicroTime) String() string {
	return time.Time(*j).Format("2006-01-02")
}

type opmlBuilder struct {
	Notes []*Note
}

func (o *opmlBuilder) AddNote(note *Note) {
	o.Notes = append(o.Notes, note)
}

func (o *opmlBuilder) String() string {
	tmpl, err := template.New("text_file").Funcs(template.FuncMap{
		"escapeXML": func(s string) string {
			sb := strings.Builder{}
			xml.Escape(&sb, []byte(s))
			return sb.String()
		},
	}).Parse(`
{{- define "DynoDate"}}!({{.}}){{end -}}
{{- define "TagList"}}{{range .}} #{{.Name}}{{end}}{{end -}}
{{- /* start of file */ -}}
<?xml version="1.0" encoding="utf-8"?>
<opml version="2.0">
  <head>
    <title></title>
    <flavor>dynalist</flavor>
    <source>https://github.com/dragon1672</source>
    <ownerName>One Smart Cookie</ownerName>
  </head>
  <body>
    <outline text="Google Keep Export">
{{- range .Notes }}
        <outline text="{{.Title | escapeXML}}" _note="{{template "DynoDate" .CreatedMicros}}{{template "TagList" .Labels}}">
		{{- with .TextContent}}
            <outline text="---" _note="{{. | escapeXML}}"/>
		{{- end}}
		{{- with .ListContent}}{{range .}}
            <outline text="{{.Text | escapeXML}}"{{if .IsChecked}} complete="true"{{end}}/>{{end}}
		{{- end}}
{{- end}} {{/* end of Notes range */}}
    </outline>
  </body>
</opml>

{{- /* end of file */ -}}
`)
	if err != nil {
		panic(err)
	}

	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, o); err != nil {
		panic(err)
	}
	return sb.String()
}

func (n *Note) String() string {
	tmpl, err := template.New("text_file").Parse(`
{{- define "DynoDate"}}!({{.}}){{end -}}
{{- define "ListCheck"}}[{{if .IsChecked}}X{{else}} {{end}}]{{end -}}
{{- define "ListEntry"}}{{template "ListCheck" .}} - {{.Text}}{{end -}}
{{- /* start of file */ -}}
{{- .Title}}
{{- with .CreatedMicros}}
Created: {{.}}{{end}}
{{- with .EditedMicros}}
Edited: {{.}}{{end}}

{{with .TextContent}}{{.}}
{{end}}
{{- with .ListContent}}{{range .}}{{template "ListEntry" .}}
{{end}}
{{- end}}{{- /* end of body */}} 

{{- with .Labels}}
{{range .}}#{{.Name}}
{{end}}{{end}}
{{- /* end of file */ -}}
`)
	if err != nil {
		panic(err)
	}

	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, n); err != nil {
		panic(err)
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

func writeFile(data string, destination string, createDir bool) error {
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

	if _, err := destinationFile.WriteString(data); err != nil {
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

// validateAndConvertZipFileToNote will return nil if the file should be skipped.
func validateAndConvertZipFileToNote(file *zip.File) (*Note, error) {
	if path.Ext(file.Name) != ".json" {
		return nil, nil // skip
	}
	if !strings.Contains(file.Name, *SubFolderPath) {
		return nil, nil // skip
	}
	note, err := file2Note(file)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %v", file.Name, err)
	}
	if note.IsTrashed || note.IsArchived {
		glog.Infof("skipping trashed or archived entry %v", file.Name)
		return nil, nil // skip the dead stuffs
	}
	return note, nil
}

func main() {
	flag.Parse()

	opmlBld := opmlBuilder{}

	if err := processZipSource(*ZipFilePath, func(file *zip.File) error {
		note, err := validateAndConvertZipFileToNote(file)
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", file.Name, err)
		}
		if note == nil {
			return nil // skip
		}

		if *StdOut {
			fmt.Printf("```note\n%s\n```\n", note)
		}

		if len(*OutputDir) > 0 {
			trimmedName := strings.TrimSuffix(filepath.Base(file.FileInfo().Name()), filepath.Ext(file.FileInfo().Name()))
			filePath, err := filepath.Abs(filepath.Join(*OutputDir, trimmedName+".txt"))
			if err != nil {
				return err
			}
			if err := writeFile(note.String(), filePath, *CreateOut); err != nil {
				return fmt.Errorf("error writing file %s: %v", file.Name, err)
			}
		}

		if len(*OutputOPMLFile) > 0 {
			opmlBld.AddNote(note)
		}

		return nil
	}); err != nil {
		log.Fatal(err)
	}

	if len(*OutputOPMLFile) > 0 {
		data := opmlBld.String()
		if err := writeFile(data, *OutputOPMLFile, *CreateOut); err != nil {
			log.Fatalf("error writing file %s: %v", *OutputOPMLFile, err)
		}
		if *StdOut {
			fmt.Printf("```opml\n%s\n```\n", data)
		}
	}
}
