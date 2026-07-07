package game

import (
	"strings"
	"testing"

	"story-points/internal/db"
)

func TestOptions(t *testing.T) {
	opts := Options()
	want := []string{"?", "0", "1", "2", "3", "5", "8", "13", "21", "34", "55", "89"}
	if len(opts) != len(want) {
		t.Fatalf("esperaba %d opciones, obtuve %d", len(want), len(opts))
	}
	for i, v := range want {
		if opts[i] != v {
			t.Fatalf("esperaba %q en posición %d, obtuve %q", v, i, opts[i])
		}
	}
}

func TestGenerateID(t *testing.T) {
	id, err := GenerateID()
	if err != nil {
		t.Fatalf("generate id: %v", err)
	}
	if len(id) != 16 {
		t.Fatalf("esperaba id de 16 caracteres, obtuvo %d", len(id))
	}
	if strings.ContainsAny(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ/") {
		t.Fatalf("el id contiene caracteres no permitidos: %s", id)
	}
}

func TestNumericValue(t *testing.T) {
	tests := []struct {
		input string
		want  float64
		ok    bool
	}{
		{"5", 5, true},
		{"13", 13, true},
		{"?", 0, false},
		{"abc", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := NumericValue(tt.input)
			if ok != tt.ok {
				t.Fatalf("esperaba ok=%v, obtuve ok=%v", tt.ok, ok)
			}
			if ok && got != tt.want {
				t.Fatalf("esperaba %v, obtuve %v", tt.want, got)
			}
		})
	}
}

func TestAverage(t *testing.T) {
	votes := []db.Vote{
		{Value: "5"},
		{Value: "8"},
		{Value: "?"},
		{Value: "13"},
	}

	got := Average(votes)
	want := 8.666666666666666
	if got != want {
		t.Fatalf("esperaba %v, obtuve %v", want, got)
	}
}

func TestAverageEmpty(t *testing.T) {
	if got := Average(nil); got != 0 {
		t.Fatalf("esperaba 0, obtuvo %v", got)
	}
}

func TestFormatVotes(t *testing.T) {
	votes := []db.Vote{
		{Username: "ana", Value: "5"},
		{Username: "juan", Value: "8"},
	}

	got := FormatVotes(votes)
	if !strings.Contains(got, "@ana: 5") {
		t.Fatalf("esperaba mención a ana, obtuvo: %s", got)
	}
	if !strings.Contains(got, "Promedio:") {
		t.Fatalf("esperaba promedio, obtuvo: %s", got)
	}
}

func TestFormatVotesEmpty(t *testing.T) {
	got := FormatVotes(nil)
	if got != "No hay votos." {
		t.Fatalf("esperaba 'No hay votos.', obtuvo: %s", got)
	}
}

func TestVotingProgress(t *testing.T) {
	players := []db.Player{
		{UserID: 1, Username: "ana"},
		{UserID: 2, Username: "juan"},
	}
	votes := []db.Vote{{UserID: 1, Value: "5"}}

	got := VotingProgress(players, votes)
	if !strings.Contains(got, "1 de 2 jugadores votaron") {
		t.Fatalf("esperaba progreso 1 de 2, obtuvo: %s", got)
	}
	if !strings.Contains(got, "[V] @ana") {
		t.Fatalf("esperaba que ana esté marcada como votada, obtuvo: %s", got)
	}
	if !strings.Contains(got, "[ ] @juan") {
		t.Fatalf("esperaba que juan esté pendiente, obtuvo: %s", got)
	}
}

func TestDeepLink(t *testing.T) {
	got := DeepLink("mibot", "abc123")
	want := "https://t.me/mibot?start=abc123"
	if got != want {
		t.Fatalf("esperaba %q, obtuvo %q", want, got)
	}
}
