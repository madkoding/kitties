package agenda

import (
	"fmt"
	"os"
	"testing"
)

// A journal past its cap keeps its newest entries, in half the cap.
func TestJournalStaysUnderItsCap(t *testing.T) {
	old := MaxJournalBytes
	MaxJournalBytes = 4000
	t.Cleanup(func() { MaxJournalBytes = old })
	j := NewJournal(t.TempDir())
	for i := 0; i < 200; i++ {
		if err := j.Add(Entry{Kitty: "mimi", Kind: KindDone, Text: fmt.Sprintf("entry %03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := os.Stat(j.file("mimi"))
	if err != nil || st.Size() > MaxJournalBytes {
		t.Fatalf("journal = %v bytes, %v", st.Size(), err)
	}
	got := j.Recent("mimi", 0)
	if len(got) == 0 || got[0].Text != "entry 199" {
		t.Errorf("newest = %+v", got[:1])
	}
	if got[len(got)-1].Text == "entry 000" {
		t.Error("the oldest entries should have gone")
	}
}
