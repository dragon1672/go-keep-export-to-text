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

// Inputs
var (
	ZipFilePath   = flag.String("zip_file_path", "takeout-example.zip", "zip file path to be unpacked and parsed")
	SubFolderPath = flag.String("sub_folder_path", "Takeout/Keep/", "required sub folder")
)

// Outputs
var (
	StdOut         = flag.Bool("std_out", true, "optionally print contents to console")
	TxtOutputDir   = flag.String("txt_output_dir", "out", "text file output file dir. Optionally create directories controlled by --create_out")
	MdOutputDir    = flag.String("md_output_dir", "md_out", "markdown output file dir. Optionally create directories controlled by --create_out")
	OutputOPMLFile = flag.String("output_ompl_file", "out.opml", "output OPML file. Optionally create directories controlled by --create_out")
)

const (
	StratDirectExport = "direct_export"
	StratFavorDate    = "favor_date" // attempt to only include the date (YYYY-MM-DD) will fall back to date prefixed `YYYY-MM-DD_${direct_export}`
)

// Configurations
var (
	FileNameStrat      = flag.String("output_file_name_strat", StratFavorDate, "How to resolve file names")
	CreateYearFolders  = flag.Bool("output_create_year_folders", true, "Create sub folders for each year")
	CreateMonthFolders = flag.Bool("output_create_month_folders", true, "Create sub folders for each month (requires --output_create_year_folders, otherwise is ignored) This will include both the month number (0 padded), and the month name")
	CreateOut          = flag.Bool("create_out", true, "Attempt to create output dir")
	DefaultTags        = flag.String("default_tags", "google_keep_export", "comma seperated list of default tags to apply to all tags")
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
func (j *MicroTime) Time() time.Time {
	return time.Time(*j)
}
func (j *MicroTime) String() string {
	return j.Time().Format("2006-01-02")
}

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

// ==================
//       Writers
// ==================

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

type fileNameGenerator struct {
	GenerateYearFolders  bool
	GenerateMonthFolders bool
	NameStrat            string

	mu            sync.Mutex
	reservedPaths map[string]bool
}

func (f *fileNameGenerator) GenerateAndReserve(n *Note) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reservedPaths == nil {
		f.reservedPaths = make(map[string]bool)
	}
	fileName := n.FileName // default to STRAT_DIRECT_EXPORT
	if f.NameStrat == StratDirectExport {
		fileName = n.FileName
	} else if f.NameStrat == StratFavorDate {
		fileName = n.CreatedMicros.String() // attempt to make just the date
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

type opmlBuilder struct {
	mu     sync.RWMutex
	notes  []*Note
	Writer *fileWriter
}

func (o *opmlBuilder) AddNote(note *Note) {
	o.mu.Lock()
	o.notes = append(o.notes, note)
	o.mu.Unlock()
}
func (o *opmlBuilder) ToOPML() string {
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
func (o *opmlBuilder) WriteOPML(outFile string) error {
	return o.Writer.WriteFile(o.ToOPML(), outFile)
}

type TextFileWriter struct {
	writer    *fileWriter
	generator *fileNameGenerator
	outDir    string
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
	fileName := t.generator.GenerateAndReserve(n)
	filePath, err := filepath.Abs(filepath.Join(t.outDir, fileName+".txt"))
	if err != nil {
		return err
	}
	return t.writer.WriteFile(t.note2Txt(n), filePath)
}

type MdFileWriter struct {
	writer    *fileWriter
	generator *fileNameGenerator
	outDir    string
}

func (m *MdFileWriter) note2Md(n *Note) string {
	tmpl, err := template.New("text_file").Parse(`
{{- define "ListCheck"}}[{{if .IsChecked}}x{{else}} {{end}}]{{end -}}
{{- define "ListEntry"}} - {{template "ListCheck" .}} {{.Text}}{{end -}}
{{- /* start of file */ -}}
# {{.Title}}{{- with .CreatedMicros}} - [[{{.}}]]
Created: [[{{.}}]]{{end}}
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
	fileName := m.generator.GenerateAndReserve(n)
	filePath, err := filepath.Abs(filepath.Join(m.outDir, fileName+".md"))
	if err != nil {
		return err
	}
	return m.writer.WriteFile(m.note2Md(n), filePath)
}

func main() {
	flag.Parse()

	reader := ZipToNoteReader{
		SubFolderPath: *SubFolderPath,
		DefaultTags:   strings.Split(*DefaultTags, ","),
	}
	writer := &fileWriter{
		CreateDir: *CreateOut,
		Stdout:    *StdOut,
	}
	fileGenerator := &fileNameGenerator{
		GenerateYearFolders:  *CreateYearFolders,
		GenerateMonthFolders: *CreateMonthFolders,
		NameStrat:            *FileNameStrat,
	}

	var opmlBld *opmlBuilder
	if len(*OutputOPMLFile) > 0 {
		opmlBld = &opmlBuilder{Writer: writer}
	}
	var txtWriter *TextFileWriter
	if len(*TxtOutputDir) > 0 {
		txtWriter = &TextFileWriter{
			writer:    writer,
			generator: fileGenerator,
			outDir:    *TxtOutputDir,
		}
	}

	var mdWriter *MdFileWriter
	if len(*MdOutputDir) > 0 {
		mdWriter = &MdFileWriter{
			writer:    writer,
			generator: fileGenerator,
			outDir:    *MdOutputDir,
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

	if opmlBld != nil {
		if err := opmlBld.WriteOPML(*OutputOPMLFile); err != nil {
			log.Fatalf("error writing file %s: %v", *OutputOPMLFile, err)
		}
	}
}
