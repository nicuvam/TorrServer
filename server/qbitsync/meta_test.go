package qbitsync

import "testing"

func TestPrettyName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ted.Lasso.S04.2160p.ATVP.WEB-DL.SDR.H.265", "Ted Lasso S04 2160p ATVP WEB-DL SDR H 265"},
		{"Some_Show_S01_1080p", "Some Show S01 1080p"},
		{"Малой (Копы) / Сезон: 1 / Серии: 1-16 из 16", "Малой (Копы) / Сезон: 1 / Серии: 1-16 из 16"},
		{"", ""},
	}
	for _, test := range tests {
		if got := prettyName(test.in); got != test.want {
			t.Fatalf("prettyName(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
