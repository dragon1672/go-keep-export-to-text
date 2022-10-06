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
	"sync"
	"text/template"
	"time"

	"github.com/golang/glog"
	"golang.org/x/sync/errgroup"
)

var (
	ZipFilePath    = flag.String("zip_file_path", "takeout-example.zip", "zip file path to be unpacked and parsed")
	SubFolderPath  = flag.String("sub_folder_path", "Takeout/Keep/", "required sub folder")
	StdOut         = flag.Bool("std_out", true, "optionally print contents to console")
	TxtOutputDir   = flag.String("txt_output_dir", "out", "text file output file dir. Optionally create controlled by --create_out")
	MdOutputDir    = flag.String("md_output_dir", "md_out", "markdown output file dir. Optionally create controlled by --create_out")
	OutputOPMLFile = flag.String("output_ompl_file", "out.opml", "output OPML file. Optionally create controlled by --create_out")
	CreateOut      = flag.Bool("create_out", true, "Attempt to create output dir")
)

type ListItem struct {
	Text      string `json:"text"`
	IsChecked bool   `json:"isChecked"`
}

type ListLabel struct {
	Name string `json:"name"`
}

type Note struct {
	FileName string
	// parsed fields
	Title         string      `json:"title"`
	TextContent   string      `json:"textContent"`
	IsTrashed     bool        `json:"isTrashed"`
	IsArchived    bool        `json:"isArchived"`
	ListContent   []ListItem  `json:"listContent"`
	Labels        []ListLabel `json:"labels"`
	EditedMicros  *MicroTime  `json:"userEditedTimestampUsec"`
	CreatedMicros *MicroTime  `json:"createdTimestampUsec"`
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
	mu    sync.RWMutex
	notes []*Note
}

func (o *opmlBuilder) AddNote(note *Note) {
	o.mu.Lock()
	o.notes = append(o.notes, note)
	o.mu.Unlock()
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
{{- range . }}
        <outline text="{{.Title | escapeXML}}" _note="{{template "DynoDate" .CreatedMicros}}{{template "TagList" .Labels}}">
		{{- with .TextContent}}
            <outline text="---" _note="{{. | escapeXML}}"/>
		{{- end}}
		{{- with .ListContent}}{{range .}}
            <outline text="{{.Text | escapeXML}}"{{if .IsChecked}} complete="true"{{end}}/>{{end}}
		{{- end}}
{{- end}} {{/* end of notes range */}}
    </outline>
  </body>
</opml>

{{- /* end of file */ -}}
`)
	if err != nil {
		panic(err)
	}

	o.mu.RLock()
	defer o.mu.RUnlock()
	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, o.notes); err != nil {
		panic(err)
	}
	return sb.String()
}

type ZipToNoteReader struct {
	SubFolderPath string
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

	data, err := ioutil.ReadAll(zippedFile)
	if err != nil {
		return nil, err
	}
	note := &Note{
		FileName: strings.TrimSuffix(filepath.Base(f.FileInfo().Name()), filepath.Ext(f.FileInfo().Name())),
	}
	if err := json.Unmarshal(data, note); err != nil {
		return nil, err
	}
	if note.Title == "" {
		glog.Infof("providing default title for file %v", f.FileInfo().Name())
		note.Title = f.FileInfo().Name()
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

type fileWriter struct {
	CreateDir bool
	Stdout    bool // also write to std out
}

func (f *fileWriter) WriteFile(data string, destination string) error {
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

type TextFileWriter struct {
	writer *fileWriter
	outDir string
}

func (t *TextFileWriter) note2Txt(n *Note) string {
	tmpl, err := template.New("text_file").Parse(`
{{- define "ListCheck"}}[{{if .IsChecked}}x{{else}} {{end}}]{{end -}}
{{- define "ListEntry"}} - {{template "ListCheck" .}} {{.Text}}{{end -}}
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
func (t *TextFileWriter) WriteNote(n *Note) error {
	filePath, err := filepath.Abs(filepath.Join(t.outDir, n.FileName+".txt"))
	if err != nil {
		return err
	}
	return t.writer.WriteFile(t.note2Txt(n), filePath)
}

type MdFileWriter struct {
	writer *fileWriter
	outDir string
}

func (m *MdFileWriter) note2Txt(n *Note) string {
	tmpl, err := template.New("text_file").Parse(`
{{- define "ListCheck"}}[{{if .IsChecked}}x{{else}} {{end}}]{{end -}}
{{- define "ListEntry"}} - {{template "ListCheck" .}} {{.Text}}{{end -}}
{{- /* start of file */ -}}
# {{.Title}}{{- with .CreatedMicros}} - {{.}}{{end}}
{{- with .EditedMicros}}
Last Edited: {{.}}{{end}}

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
func (m *MdFileWriter) WriteNote(n *Note) error {
	filePath, err := filepath.Abs(filepath.Join(m.outDir, n.FileName+".md"))
	if err != nil {
		return err
	}
	return m.writer.WriteFile(m.note2Txt(n), filePath)
}

func main() {
	flag.Parse()

	reader := ZipToNoteReader{
		SubFolderPath: *SubFolderPath,
	}
	writer := &fileWriter{
		CreateDir: *CreateOut,
		Stdout:    *StdOut,
	}

	var opmlBld *opmlBuilder
	if len(*OutputOPMLFile) > 0 {
		opmlBld = &opmlBuilder{}
	}
	var txtWriter *TextFileWriter
	if len(*TxtOutputDir) > 0 {
		txtWriter = &TextFileWriter{
			writer: writer,
			outDir: *TxtOutputDir,
		}
	}

	var mdWriter *MdFileWriter
	if len(*MdOutputDir) > 0 {
		mdWriter = &MdFileWriter{
			writer: writer,
			outDir: *MdOutputDir,
		}
	}

	g := new(errgroup.Group)
	if err := reader.StreamNotes(*ZipFilePath, func(note *Note) error {
		n := note // local ref
		if *StdOut {
			fmt.Printf("```note\n%+v\n```\n", n)
		}

		if txtWriter != nil {
			g.Go(func() error {
				return txtWriter.WriteNote(n)
			})
		}

		if mdWriter != nil {
			g.Go(func() error {
				return mdWriter.WriteNote(n)
			})
		}

		if opmlBld != nil {
			g.Go(func() error {
				opmlBld.AddNote(n)
				return nil
			})
		}

		return nil
	}); err != nil {
		log.Fatal(err)
	}
	if err := g.Wait(); err != nil {
		log.Fatal(err)
	}

	if len(*OutputOPMLFile) > 0 {
		data := opmlBld.String()
		if err := writer.WriteFile(data, *OutputOPMLFile); err != nil {
			log.Fatalf("error writing file %s: %v", *OutputOPMLFile, err)
		}
	}
}
