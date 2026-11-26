package tui

import (
	"fmt"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

const (
	benchWidth  = 150
	benchHeight = 40
	benchGroups = 20
	benchQuery  = "bloom"
)

var benchSizes = []int{1_000, 100_000}

// benchCase is a configuration drawn at each size; the ratio of the two sizes is what the benchmarks show.
type benchCase struct {
	name    string
	grouped bool
	filter  string
}

var benchCases = []benchCase{
	{name: "flat"},
	{name: "grouped", grouped: true},
	{name: "filtered", grouped: true, filter: benchQuery},
}

// benchPlants are n invented entries in benchGroups groups, half of them blooming.
func benchPlants(n int) []ListItem {
	notes := []string{"", "shade", "spreads fast", "on the sill by the kitchen window", "planted long ago"}
	items := make([]ListItem, n)
	for i := range items {
		id := fmt.Sprintf("plant%06d", i)
		state := "resting"
		if i%2 == 0 {
			state = "blooming"
		}
		items[i] = ListItem{ID: ItemID(id), Group: fmt.Sprintf("bed%02d", i%benchGroups),
			Cells: []string{id, state, fmt.Sprint(i % 1000), notes[i%len(notes)]}}
	}
	return items
}

// benchList is a list of n entries; a grouped one has titled groups, ticks on every third entry and the row
// of all entries.
func benchList(b *testing.B, n int, c benchCase) *List {
	b.Helper()
	keys, err := NewKeymap(sampleActions(), Styles{}.Charset())
	if err != nil {
		b.Fatal(err)
	}
	catalogs := listCatalogs()
	catalogs[i18n.English]["m.title"] = "Pick plants"
	text, err := NewTranslator(i18n.English, catalogs, keys)
	if err != nil {
		b.Fatal(err)
	}
	spec := ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty", NoGroup: "list.nogroup",
		Title: "m.title", Ticks: c.grouped, AllRow: c.grouped}
	l, err := NewList(spec, keys, text)
	if err != nil {
		b.Fatal(err)
	}
	items := benchPlants(n)
	if err := l.SetItems(items); err != nil {
		b.Fatal(err)
	}
	if c.grouped {
		var groups []ListGroup
		for g := range benchGroups {
			groups = append(groups, ListGroup{Key: fmt.Sprintf("bed%02d", g), Title: fmt.Sprintf("Bed %d", g+1)})
		}
		if err := l.SetGroups(groups); err != nil {
			b.Fatal(err)
		}
		l.SetGrouped(true)
		var ticked []ItemID
		for i := 0; i < n; i += 3 {
			ticked = append(ticked, items[i].ID)
		}
		if err := l.SetTicked(ticked); err != nil {
			b.Fatal(err)
		}
	}
	l.SetFilter(c.filter)
	return l
}

func BenchmarkListView(b *testing.B) {
	for _, c := range benchCases {
		for _, n := range benchSizes {
			b.Run(fmt.Sprintf("%s/n=%d", c.name, n), func(b *testing.B) {
				l := benchList(b, n, c)
				for b.Loop() {
					l.View(Styles{}, benchWidth, benchHeight)
				}
			})
		}
	}
}

func BenchmarkListOverlay(b *testing.B) {
	screen := sampleScreen(benchWidth, benchHeight)
	room := Rect{Width: benchWidth, Height: benchHeight}
	for _, c := range benchCases {
		for _, n := range benchSizes {
			b.Run(fmt.Sprintf("%s/n=%d", c.name, n), func(b *testing.B) {
				l := benchList(b, n, c)
				for b.Loop() {
					if _, _, ok := l.Overlay(Styles{}, screen, benchWidth, benchHeight, room); !ok {
						b.Fatal("the modal does not fit")
					}
				}
			})
		}
	}
}

// BenchmarkListSetFilter alternates two queries as typing and deleting a letter does.
func BenchmarkListSetFilter(b *testing.B) {
	b.Run("grouped/n=100000", func(b *testing.B) {
		l := benchList(b, 100_000, benchCases[1])
		queries := []string{benchQuery, benchQuery + "i"}
		i := 0
		for b.Loop() {
			l.SetFilter(queries[i%2])
			i++
		}
	})
}

func BenchmarkListSetItems(b *testing.B) {
	b.Run("grouped/n=100000", func(b *testing.B) {
		l := benchList(b, 100_000, benchCases[1])
		items := benchPlants(100_000)
		for b.Loop() {
			if err := l.SetItems(items); err != nil {
				b.Fatal(err)
			}
		}
	})
}
