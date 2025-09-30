package opml

import (
	"strings"
	"sync"

	"encoding/xml"
	"text/template"

	"github.com/dragon1672/go-keep-export-to-text/keep"
	"github.com/dragon1672/go-keep-export-to-text/keep/loader"
)

// Builder buffers notes in memory then writes to a single file.
type Builder struct {
	OutputFile string
	Writer    *keep.FileWriter
	
	mu        sync.RWMutex
	notes     []*loader.Note
	writeChan chan *keep.NoteWriteRequest // Channel for writing notes
	done      chan bool
}

func (b *Builder) WriteNote(note *loader.Note) error {
	b.mu.Lock()
	b.notes = append(b.notes, note)
	b.mu.Unlock()
	return nil
}

func (b *Builder) ToOPML() (string, error) {
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
		return "", err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	sb := strings.Builder{}
	if err := tmpl.Execute(&sb, b.notes); err != nil {
		return "", err
	}
	return sb.String(), nil
}
func (b *Builder) Flush() error {
	ompl, err := b.ToOPML()
	if err != nil {
		return err
	}
	return b.Writer.WriteFile(ompl, b.OutputFile)
}

var _ keep.NoteWriter = (*Builder)(nil)