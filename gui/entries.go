package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func isDirectChildName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.Contains(name, "/")
}

func (s *state) refreshEntriesUI() {
	if s.entriesBox == nil {
		return
	}
	s.entriesBox.Objects = nil
	s.entryRows = nil
	for i, raw := range s.entries {
		i := i
		raw := raw
		row := newEntryRow(raw)
		row.onTap = func() { s.selectEntry(i) }
		row.onDoubleTap = func() {
			s.selectEntry(i)
			if strings.HasSuffix(raw, "/") {
				name := strings.TrimSuffix(raw, "/")
				if isDirectChildName(name) {
					_ = s.cd(name)
				}
			}
		}
		s.entryRows = append(s.entryRows, row)
		s.entriesBox.Add(row)
	}
	s.entriesBox.Refresh()
}

func (s *state) selectEntry(i int) {
	if i < 0 || i >= len(s.entries) {
		return
	}
	raw := s.entries[i]
	entry := strings.TrimSuffix(raw, "/")

	if s.selectedIndex >= 0 && s.selectedIndex < len(s.entryRows) {
		s.entryRows[s.selectedIndex].SetSelected(false)
	}
	s.selectedIndex = i
	if i >= 0 && i < len(s.entryRows) {
		s.entryRows[i].SetSelected(true)
	}

	s.cdEntry.SetText(entry)
	s.renameEntry.SetText(entry)
	s.deleteEntry.SetText(entry)
	s.downloadEntry.SetText(entry)
}

func (s *state) clearSelectionAndFields() {
	if s.selectedIndex >= 0 && s.selectedIndex < len(s.entryRows) {
		s.entryRows[s.selectedIndex].SetSelected(false)
	}
	s.selectedIndex = -1

	s.cdEntry.SetText("")
	s.mkdirEntry.SetText("")
	s.renameEntry.SetText("")
	s.renameNameEntry.SetText("")
	s.deleteEntry.SetText("")
	s.downloadEntry.SetText("")
	s.uploadNameEntry.SetText("")
}

type entryRow struct {
	widget.BaseWidget
	text        string
	selected    bool
	onTap       func()
	onDoubleTap func()
}

func newEntryRow(text string) *entryRow {
	r := &entryRow{text: text}
	r.ExtendBaseWidget(r)
	return r
}

func (r *entryRow) SetSelected(v bool) {
	if r.selected == v {
		return
	}
	r.selected = v
	r.Refresh()
}

func (r *entryRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *entryRow) DoubleTapped(*fyne.PointEvent) {
	if r.onDoubleTap != nil {
		r.onDoubleTap()
	}
}

func (r *entryRow) CreateRenderer() fyne.WidgetRenderer {
	th := r.Theme()

	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	label := widget.NewLabel(r.text)
	label.Truncation = fyne.TextTruncateEllipsis

	content := container.NewPadded(label)
	objects := []fyne.CanvasObject{bg, content}
	renderer := &entryRowRenderer{row: r, bg: bg, label: label, content: content, objects: objects}
	renderer.Refresh()
	return renderer
}

type entryRowRenderer struct {
	row     *entryRow
	bg      *canvas.Rectangle
	label   *widget.Label
	content *fyne.Container
	objects []fyne.CanvasObject
}

func (r *entryRowRenderer) MinSize() fyne.Size { return r.content.MinSize() }

func (r *entryRowRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.content.Resize(size)
}

func (r *entryRowRenderer) Refresh() {
	th := r.row.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	r.label.SetText(r.row.text)
	if r.row.selected {
		r.bg.FillColor = th.Color(theme.ColorNameSelection, v)
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	r.content.Refresh()
}

func (r *entryRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *entryRowRenderer) Destroy()                     {}
