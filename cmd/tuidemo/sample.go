package main

// sampleRow is one entry of the invented garden journal the tabs show.
type sampleRow struct{ Name, Bed, Watered string }

// sampleRows is invented data: no real plants, places or people.
func sampleRows() []sampleRow {
	return []sampleRow{
		{"Basil", "East bed", "2031-05-04"},
		{"Mint", "Pot 3", "2031-05-06"},
		{"Tomato", "South bed", "2031-05-05"},
		{"Chive", "Pot 1", "2031-05-02"},
		{"Sage", "East bed", "2031-04-30"},
		{"Thyme", "Pot 2", "2031-05-01"},
		{"Parsley", "South bed", "2031-05-06"},
		{"Fennel", "North bed", "2031-04-28"},
	}
}
