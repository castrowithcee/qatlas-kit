package highlight

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The goldens fix the text view of a highlighted file byte for byte. Run with -update to rewrite them.
func TestGoldenTextView(t *testing.T) {
	for _, c := range []struct{ name, file, golden string }{
		{"compose.yaml", "compose.yaml", "textview.compose.golden"},
		{".env", "example.env", "textview.env.golden"},
	} {
		t.Run(c.golden, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", c.file))
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			spans := Highlight(c.name, text)
			if spans == nil {
				t.Fatal("no sections")
			}
			view := newView(t)
			view.SetText(text, spans)
			out, _ := view.View(styles(t), 64, 34)
			got := out + "\n"
			golden := filepath.Join("testdata", c.golden)
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("view differs from %s:\ngot:\n%s\nwant:\n%s", golden, got, want)
			}
		})
	}
}
