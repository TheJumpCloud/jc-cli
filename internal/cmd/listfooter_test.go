package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func footerOf(t *testing.T, write func(*cobra.Command)) string {
	t.Helper()
	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	write(cmd)
	return errBuf.String()
}

func TestWriteCountFooter(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		// A one-row list used to read "── 1 items ──" at 56 call sites.
		{1, "── 1 item ──\n"},
		{0, "── 0 items ──\n"},
		{2, "── 2 items ──\n"},
	}
	for _, c := range cases {
		got := footerOf(t, func(cmd *cobra.Command) { writeCountFooter(cmd, c.count) })
		if got != c.want {
			t.Errorf("writeCountFooter(%d) = %q, want %q", c.count, got, c.want)
		}
	}
}

func TestWriteNounFooter(t *testing.T) {
	// "saved search" is why writeNounFooter takes both forms: an existing
	// test caught "── 2 saved searchs ──" when it derived the plural.
	got := footerOf(t, func(cmd *cobra.Command) {
		writeNounFooter(cmd, 2, "saved search", "saved searches")
	})
	if want := "── 2 saved searches ──\n"; got != want {
		t.Errorf("writeNounFooter = %q, want %q", got, want)
	}
	got = footerOf(t, func(cmd *cobra.Command) {
		writeNounFooter(cmd, 1, "saved search", "saved searches")
	})
	if want := "── 1 saved search ──\n"; got != want {
		t.Errorf("writeNounFooter = %q, want %q", got, want)
	}
}

func TestWriteListFooter(t *testing.T) {
	cases := []struct {
		count, total int
		want         string
	}{
		// A full page collapses to the plain count.
		{3, 3, "── 3 items ──\n"},
		{1, 1, "── 1 item ──\n"},
		// A short page names the total, and pluralises on it.
		{1, 5, "── 1 of 5 items ──\n"},
		{0, 1, "── 0 of 1 item ──\n"},
		{25, 300, "── 25 of 300 items ──\n"},
	}
	for _, c := range cases {
		got := footerOf(t, func(cmd *cobra.Command) { writeListFooter(cmd, c.count, c.total) })
		if got != c.want {
			t.Errorf("writeListFooter(%d, %d) = %q, want %q", c.count, c.total, got, c.want)
		}
	}
}
