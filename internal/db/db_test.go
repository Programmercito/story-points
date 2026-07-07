package db

import (
	"database/sql"
	"testing"
)

func newTestDB(t *testing.T) *sql.DB {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateGameCreatesCreatorAsPlayer(t *testing.T) {
	db := newTestDB(t)

	g, err := CreateGame(db, "game1", 1, "creator")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if g.Status != GameStatusWaiting {
		t.Fatalf("esperaba estado waiting, obtuve %s", g.Status)
	}

	players, err := GetActivePlayers(db, g.ID)
	if err != nil {
		t.Fatalf("get players: %v", err)
	}
	if len(players) != 1 {
		t.Fatalf("esperaba 1 jugador, obtuve %d", len(players))
	}
	if players[0].Username != "creator" {
		t.Fatalf("esperaba creator, obtuve %s", players[0].Username)
	}
}

func TestGetGameNotFound(t *testing.T) {
	db := newTestDB(t)

	g, err := GetGame(db, "noexiste")
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if g != nil {
		t.Fatalf("esperaba nil, obtuve %+v", g)
	}
}

func TestCastVoteDoesNotAllowChanging(t *testing.T) {
	db := newTestDB(t)

	if _, err := CreateGame(db, "vote1", 1, "creator"); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := StartGame(db, "vote1"); err != nil {
		t.Fatalf("start game: %v", err)
	}
	if err := CastVote(db, "vote1", 1, "creator", "5"); err != nil {
		t.Fatalf("first cast vote: %v", err)
	}
	if err := CastVote(db, "vote1", 1, "creator", "8"); err == nil {
		t.Fatal("esperaba error al votar dos veces")
	}

	votes, err := GetVotes(db, "vote1")
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 1 || votes[0].Value != "5" {
		t.Fatalf("esperaba 1 voto con 5, obtuvo %+v", votes)
	}
}

func TestDeleteVotesResetsVotes(t *testing.T) {
	db := newTestDB(t)

	if _, err := CreateGame(db, "reset1", 1, "creator"); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := StartGame(db, "reset1"); err != nil {
		t.Fatalf("start game: %v", err)
	}
	if err := CastVote(db, "reset1", 1, "creator", "5"); err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	if err := DeleteVotes(db, "reset1"); err != nil {
		t.Fatalf("delete votes: %v", err)
	}

	votes, err := GetVotes(db, "reset1")
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 0 {
		t.Fatalf("esperaba 0 votos, obtuvo %d", len(votes))
	}
}

func TestRevealVotesChangesStatus(t *testing.T) {
	db := newTestDB(t)

	if _, err := CreateGame(db, "reveal1", 1, "creator"); err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := StartGame(db, "reveal1"); err != nil {
		t.Fatalf("start game: %v", err)
	}
	if err := RevealVotes(db, "reveal1"); err != nil {
		t.Fatalf("reveal votes: %v", err)
	}

	g, err := GetGame(db, "reveal1")
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if g.Status != GameStatusRevealed {
		t.Fatalf("esperaba estado revealed, obtuve %s", g.Status)
	}
}
