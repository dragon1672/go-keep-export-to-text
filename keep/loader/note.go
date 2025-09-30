package loader

import (
	"strconv"
	"time"
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
	Title          string `json:"title"`
	ExtractedTitle string
	TextContent    string      `json:"textContent"`
	IsTrashed      bool        `json:"isTrashed"`
	IsArchived     bool        `json:"isArchived"`
	ListContent    []ListItem  `json:"listContent"`
	Labels         []ListLabel `json:"labels"`
	EditedMicros   *MicroTime  `json:"userEditedTimestampUsec"`
	CreatedMicros  *MicroTime  `json:"createdTimestampUsec"`
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

