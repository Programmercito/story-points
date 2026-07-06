package game

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"story-points/internal/db"
)

const StoryPointOptions = "?,0,1,2,3,5,8,13,21,34,55,89"

func Options() []string {
	return strings.Split(StoryPointOptions, ",")
}

func GenerateID() (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 16)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", fmt.Errorf("generate id: %w", err)
		}
		b[i] = letters[n.Int64()]
	}
	return string(b), nil
}

func DeepLink(botUsername, gameID string) string {
	return fmt.Sprintf("https://t.me/%s?start=%s", botUsername, gameID)
}

func NumericValue(value string) (float64, bool) {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func Average(votes []db.Vote) float64 {
	var sum float64
	var count int
	for _, v := range votes {
		if n, ok := NumericValue(v.Value); ok {
			sum += n
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func FormatVotes(votes []db.Vote) string {
	if len(votes) == 0 {
		return "No hay votos."
	}
	var b strings.Builder
	for _, v := range votes {
		fmt.Fprintf(&b, "- @%s: %s\n", v.Username, v.Value)
	}
	avg := Average(votes)
	fmt.Fprintf(&b, "\nPromedio: %.2f", math.Round(avg*100)/100)
	return b.String()
}

func VotingProgress(activePlayers []db.Player, votes []db.Vote) string {
	voted := make(map[int64]bool)
	for _, v := range votes {
		voted[v.UserID] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Progreso: %d de %d jugadores votaron.\n\n", len(votes), len(activePlayers))
	for _, p := range activePlayers {
		if voted[p.UserID] {
			fmt.Fprintf(&b, "[V] @%s - votó\n", p.Username)
		} else {
			fmt.Fprintf(&b, "[ ] @%s - pendiente\n", p.Username)
		}
	}
	return b.String()
}
