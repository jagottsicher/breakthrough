package viewer

import "testing"

func TestSniffTextVsBinary(t *testing.T) {
	cases := []struct {
		name   string
		sample []byte
		want   Kind
	}{
		{"empty", []byte{}, KindText},
		{"plain ascii", []byte("package main\n\nfunc main() {}\n"), KindText},
		{"utf8 with accents", []byte("caf\xc3\xa9, \xe2\x80\x94 em dash"), KindText},
		{"nul byte present", []byte("PNG\x00\x00\x00\rIHDR"), KindUnsupported},
		{"nul byte at the very end", []byte("trailing\x00"), KindUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sniff(c.sample); got != c.want {
				t.Errorf("Sniff(%q) = %v, want %v", c.sample, got, c.want)
			}
		})
	}
}
