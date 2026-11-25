package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/castrowithcee/qatlas-kit/tui"
)

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

// sampleFile is one invented file of the sample folder the Picker tabs show; Path uses "/" below the folder.
type sampleFile struct{ Path, Text string }

// sampleFiles is invented data: the files the program writes into its own temporary folder.
func sampleFiles() []sampleFile {
	return []sampleFile{
		{"notes.txt", "Water the herbs in the morning.\n"},
		{"seeds/basil.txt", "Basil: sow in spring, warm and bright.\n"},
		{"seeds/thyme.txt", "Thyme: sow in spring, dry soil.\n"},
		{"seeds/tomato.txt", "Tomato: sow indoors, plant out in May.\n"},
		{"beds/east.txt", "East bed: basil and sage.\n"},
		{"beds/south.txt", "South bed: tomato and parsley.\n"},
		{"beds/pots/pot-1.txt", "Pot 1: chive.\n"},
	}
}

// newSampleRoot writes the sample files into a new temporary folder and returns it. The caller removes it;
// on an error nothing is left behind. The program reads and writes nothing outside this folder.
func newSampleRoot() (string, error) {
	root, err := os.MkdirTemp("", "tuidemo-")
	if err != nil {
		return "", err
	}
	for _, f := range sampleFiles() {
		p := filepath.Join(root, filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
			err = os.WriteFile(p, []byte(f.Text), 0o600)
		}
		if err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
	}
	return root, nil
}

// sampleSecret is the invented secret of the sample configuration; the Text tab masks it.
const sampleSecret = "example-not-a-secret"

// sampleConfig is an invented configuration the Text tab shows: example.org and 192.0.2.0/24 are reserved for
// documentation.
const sampleConfig = `# Sample configuration of an invented garden sensor service.
server:
  host: example.org
  port: 8443
  tls: true
sensors:
  - name: east-bed
    address: 192.0.2.10
    interval: 30
  - name: south-bed
    address: 192.0.2.11
    interval: 60
  - name: pot-row
    address: 192.0.2.12
    interval: 120
notify:
  token: example-not-a-secret
  retries: 3
  note: "A long line shows what the wrap switch does: it continues on the next row instead of being cut at the edge of the view."
logging:
  level: info
  file: garden.log
`

// sampleLog returns invented log lines, one every 47 minutes up to the clock sampleToday, so a period such as
// the last hour or day holds some of them and not all.
func sampleLog() []tui.LogLine {
	texts := []struct{ source, text string }{
		{"sensor", "east-bed reading 18.4 C from 192.0.2.10"},
		{"sensor", "south-bed reading 19.1 C from 192.0.2.11"},
		{"pump", "pump started for pot-row, 40 s"},
		{"api", "GET https://example.org/beds 200 12 ms"},
		{"sensor", "pot-row reading 17.9 C from 192.0.2.12"},
		{"pump", "pump stopped, 1.2 l delivered"},
		{"api", "GET https://example.org/readings 200 31 ms"},
		{"sensor", "east-bed moisture low, below 30 percent"},
		{"api", "POST https://example.org/notify 202 8 ms"},
		{"sensor", "south-bed reading timed out, retry 1 of 3"},
		{"api", "GET https://example.org/beds 304 4 ms, the reply of this line is long enough to show what the wrap switch does when it is on"},
	}
	const n = 60
	now := sampleToday()
	lines := make([]tui.LogLine, n)
	for i := range lines {
		t := texts[i%len(texts)]
		lines[i] = tui.LogLine{Time: now.Add(-time.Duration(n-1-i) * 47 * time.Minute), Source: t.source, Text: t.text}
	}
	return lines
}

// sampleChat returns the invented messages the Chat tab starts with.
func sampleChat() []tui.ChatMessage {
	at := sampleToday().Add(-30 * time.Minute)
	return []tui.ChatMessage{
		{ID: "m1", From: "Rosa", At: at, Text: "Good morning. The east bed looks dry today."},
		{ID: "m2", From: "You", At: at.Add(4 * time.Minute), Mine: true, Status: "read", Kind: tui.StatusOK,
			Text: "I will water it after lunch.\nShould I add mulch as well?"},
		{ID: "m3", From: "Rosa", At: at.Add(9 * time.Minute), Text: "Yes, mulch keeps the soil moist.",
			Details: []string{"Mulch: a layer of straw, 5 cm.", "Keep it away from the stems of the basil."}},
	}
}
