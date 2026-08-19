package contextsummary_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
)

func TestTokenEstimate(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero maps zero", in: 0, want: 0},
		{name: "one byte maps one", in: 1, want: 1},
		{name: "two bytes map one", in: 2, want: 1},
		{name: "four bytes map one", in: 4, want: 1},
		{name: "five bytes floor to one", in: 5, want: 1},
		{name: "seven bytes floor to one", in: 7, want: 1},
		{name: "eight bytes divide cleanly", in: 8, want: 2},
		{name: "forty bytes divide cleanly", in: 40, want: 10},
		{name: "negative maps zero", in: -4, want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := contextsummary.TokenEstimate(c.in); got != c.want {
				t.Fatalf("TokenEstimate(%d) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
