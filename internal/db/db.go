package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type GameStatus string

const (
	GameStatusWaiting  GameStatus = "waiting"
	GameStatusVoting   GameStatus = "voting"
	GameStatusRevealed GameStatus = "revealed"
)

type PlayerStatus string

const (
	PlayerStatusActive PlayerStatus = "active"
	PlayerStatusLeft   PlayerStatus = "left"
)

type Game struct {
	ID              string
	CreatorID       int64
	CreatorUsername string
	Status          GameStatus
	CreatedAt       time.Time
}

type Player struct {
	ID       int64
	GameID   string
	UserID   int64
	Username string
	Status   PlayerStatus
	JoinedAt time.Time
}

type Vote struct {
	ID        int64
	GameID    string
	UserID    int64
	Username  string
	Value     string
	CreatedAt time.Time
}

func InitDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS games (
		id TEXT PRIMARY KEY,
		creator_id INTEGER NOT NULL,
		creator_username TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'waiting',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS players (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL,
		username TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(game_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS votes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL,
		username TEXT NOT NULL,
		value TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(game_id, user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_players_game ON players(game_id);
	CREATE INDEX IF NOT EXISTS idx_votes_game ON votes(game_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return db, nil
}

func CreateGame(db *sql.DB, id string, creatorID int64, creatorUsername string) (*Game, error) {
	game := &Game{
		ID:              id,
		CreatorID:       creatorID,
		CreatorUsername: creatorUsername,
		Status:          GameStatusWaiting,
		CreatedAt:       time.Now(),
	}

	_, err := db.Exec(
		`INSERT INTO games (id, creator_id, creator_username, status) VALUES (?, ?, ?, ?)`,
		game.ID, game.CreatorID, game.CreatorUsername, game.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("insert game: %w", err)
	}

	if _, err := AddPlayer(db, game.ID, creatorID, creatorUsername); err != nil {
		return nil, fmt.Errorf("add creator as player: %w", err)
	}

	return game, nil
}

func GetGame(db *sql.DB, id string) (*Game, error) {
	game := &Game{}
	row := db.QueryRow(
		`SELECT id, creator_id, creator_username, status, created_at FROM games WHERE id = ?`,
		id,
	)
	if err := row.Scan(&game.ID, &game.CreatorID, &game.CreatorUsername, &game.Status, &game.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get game: %w", err)
	}
	return game, nil
}

func AddPlayer(db *sql.DB, gameID string, userID int64, username string) (*Player, error) {
	var playerID int64
	res, err := db.Exec(
		`INSERT INTO players (game_id, user_id, username, status) VALUES (?, ?, ?, ?)
		 ON CONFLICT(game_id, user_id) DO UPDATE SET status = 'active', username = excluded.username`,
		gameID, userID, username, PlayerStatusActive,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert player: %w", err)
	}
	playerID, _ = res.LastInsertId()

	return &Player{
		ID:       playerID,
		GameID:   gameID,
		UserID:   userID,
		Username: username,
		Status:   PlayerStatusActive,
		JoinedAt: time.Now(),
	}, nil
}

func GetPlayers(db *sql.DB, gameID string) ([]Player, error) {
	rows, err := db.Query(
		`SELECT id, game_id, user_id, username, status, joined_at FROM players WHERE game_id = ? ORDER BY joined_at`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("query players: %w", err)
	}
	defer rows.Close()

	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.GameID, &p.UserID, &p.Username, &p.Status, &p.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan player: %w", err)
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

func GetActivePlayers(db *sql.DB, gameID string) ([]Player, error) {
	players, err := GetPlayers(db, gameID)
	if err != nil {
		return nil, err
	}
	var active []Player
	for _, p := range players {
		if p.Status == PlayerStatusActive {
			active = append(active, p)
		}
	}
	return active, nil
}

func PlayerExists(db *sql.DB, gameID string, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM players WHERE game_id = ? AND user_id = ?`,
		gameID, userID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check player exists: %w", err)
	}
	return count > 0, nil
}

func StartGame(db *sql.DB, gameID string) error {
	_, err := db.Exec(
		`UPDATE games SET status = ? WHERE id = ? AND status = ?`,
		GameStatusVoting, gameID, GameStatusWaiting,
	)
	if err != nil {
		return fmt.Errorf("start game: %w", err)
	}
	return nil
}

func CastVote(db *sql.DB, gameID string, userID int64, username, value string) error {
	_, err := db.Exec(
		`INSERT INTO votes (game_id, user_id, username, value) VALUES (?, ?, ?, ?)
		 ON CONFLICT(game_id, user_id) DO UPDATE SET value = excluded.value, username = excluded.username`,
		gameID, userID, username, value,
	)
	if err != nil {
		return fmt.Errorf("cast vote: %w", err)
	}
	return nil
}

func GetVotes(db *sql.DB, gameID string) ([]Vote, error) {
	rows, err := db.Query(
		`SELECT id, game_id, user_id, username, value, created_at FROM votes WHERE game_id = ? ORDER BY created_at`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("query votes: %w", err)
	}
	defer rows.Close()

	var votes []Vote
	for rows.Next() {
		var v Vote
		if err := rows.Scan(&v.ID, &v.GameID, &v.UserID, &v.Username, &v.Value, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		votes = append(votes, v)
	}
	return votes, rows.Err()
}

func HasVoted(db *sql.DB, gameID string, userID int64) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM votes WHERE game_id = ? AND user_id = ?`,
		gameID, userID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check voted: %w", err)
	}
	return count > 0, nil
}

func RevealVotes(db *sql.DB, gameID string) error {
	_, err := db.Exec(
		`UPDATE games SET status = ? WHERE id = ? AND status = ?`,
		GameStatusRevealed, gameID, GameStatusVoting,
	)
	if err != nil {
		return fmt.Errorf("reveal votes: %w", err)
	}
	return nil
}

func LeaveGame(db *sql.DB, gameID string, userID int64) error {
	_, err := db.Exec(
		`UPDATE players SET status = ? WHERE game_id = ? AND user_id = ?`,
		PlayerStatusLeft, gameID, userID,
	)
	if err != nil {
		return fmt.Errorf("leave game: %w", err)
	}
	return nil
}

func GetGamesByUser(db *sql.DB, userID int64) ([]Game, error) {
	rows, err := db.Query(
		`SELECT g.id, g.creator_id, g.creator_username, g.status, g.created_at
		 FROM games g
		 JOIN players p ON p.game_id = g.id
		 WHERE p.user_id = ? AND p.status = ?
		 ORDER BY g.created_at DESC`,
		userID, PlayerStatusActive,
	)
	if err != nil {
		return nil, fmt.Errorf("get games by user: %w", err)
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		if err := rows.Scan(&g.ID, &g.CreatorID, &g.CreatorUsername, &g.Status, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

func GetActiveGameByUser(db *sql.DB, userID int64) (*Game, error) {
	row := db.QueryRow(
		`SELECT g.id, g.creator_id, g.creator_username, g.status, g.created_at
		 FROM games g
		 JOIN players p ON p.game_id = g.id
		 WHERE p.user_id = ? AND p.status = ? AND g.status IN (?, ?)
		 ORDER BY g.created_at DESC
		 LIMIT 1`,
		userID, PlayerStatusActive, GameStatusWaiting, GameStatusVoting,
	)
	game := &Game{}
	if err := row.Scan(&game.ID, &game.CreatorID, &game.CreatorUsername, &game.Status, &game.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active game by user: %w", err)
	}
	return game, nil
}

func DeleteVotes(db *sql.DB, gameID string) error {
	_, err := db.Exec(`DELETE FROM votes WHERE game_id = ?`, gameID)
	if err != nil {
		return fmt.Errorf("delete votes: %w", err)
	}
	return nil
}
