package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"story-points/internal/db"
)

func newTestBot(t *testing.T) (*Bot, *sql.DB, *mockTelegramAPI) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	mock := &mockTelegramAPI{}
	sender := NewSender(mock)
	sender.Start()
	t.Cleanup(func() { sender.Stop() })

	b := New(mock, database, sender, "testbot")
	return b, database, mock
}

func TestHandleStartWithoutArgsShowsMainMenu(t *testing.T) {
	b, _, mock := newTestBot(t)

	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 123},
		From: &tgbotapi.User{ID: 456, UserName: "player"},
		Text: "/start",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}

	b.HandleUpdate(tgbotapi.Update{Message: msg})
	time.Sleep(100 * time.Millisecond)

	if mock.sentCount() != 1 {
		t.Fatalf("esperaba 1 mensaje enviado, obtuve %d", mock.sentCount())
	}
}

func TestHandleStartWithGameIDJoinsGame(t *testing.T) {
	b, database, _ := newTestBot(t)

	if _, err := db.CreateGame(database, "abc123", 1, "creator"); err != nil {
		t.Fatalf("create game: %v", err)
	}

	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 2},
		From: &tgbotapi.User{ID: 2, UserName: "joiner"},
		Text: "/start abc123",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 6},
		},
	}

	b.HandleUpdate(tgbotapi.Update{Message: msg})
	time.Sleep(100 * time.Millisecond)

	players, err := db.GetActivePlayers(database, "abc123")
	if err != nil {
		t.Fatalf("get players: %v", err)
	}
	if len(players) != 2 {
		t.Fatalf("esperaba 2 jugadores, obtuve %d", len(players))
	}
}

func TestHandleCreateGameCreatesGameAndSendsLink(t *testing.T) {
	b, database, mock := newTestBot(t)

	query := &tgbotapi.CallbackQuery{
		From: &tgbotapi.User{ID: 1, UserName: "creator"},
		Message: &tgbotapi.Message{
			Chat:      &tgbotapi.Chat{ID: 1},
			MessageID: 100,
		},
		Data: "create_game",
	}

	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query})
	time.Sleep(100 * time.Millisecond)

	games, err := db.GetGamesByUser(database, 1)
	if err != nil {
		t.Fatalf("get games: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("esperaba 1 juego, obtuve %d", len(games))
	}

	edits := 0
	for _, r := range mock.requested {
		if _, ok := r.(tgbotapi.EditMessageTextConfig); ok {
			edits++
		}
	}
	if edits != 1 {
		t.Fatalf("esperaba 1 edición de mensaje, obtuve %d", edits)
	}
}

func TestHandleVoteRecordsVote(t *testing.T) {
	b, database, _ := newTestBot(t)

	g, err := db.CreateGame(database, "vote123", 1, "creator")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := db.StartGame(database, g.ID); err != nil {
		t.Fatalf("start game: %v", err)
	}

	query := &tgbotapi.CallbackQuery{
		From:    &tgbotapi.User{ID: 1, UserName: "creator"},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}},
		Data:    "vote:vote123:5",
	}

	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query})
	time.Sleep(100 * time.Millisecond)

	votes, err := db.GetVotes(database, g.ID)
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("esperaba 1 voto, obtuve %d", len(votes))
	}
	if votes[0].Value != "5" {
		t.Fatalf("esperaba voto 5, obtuve %s", votes[0].Value)
	}
}

func TestHandleVoteCannotChangeVote(t *testing.T) {
	b, database, _ := newTestBot(t)

	g, err := db.CreateGame(database, "novote", 1, "creator")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := db.StartGame(database, g.ID); err != nil {
		t.Fatalf("start game: %v", err)
	}

	query1 := &tgbotapi.CallbackQuery{
		From:    &tgbotapi.User{ID: 1, UserName: "creator"},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}},
		Data:    "vote:novote:5",
	}
	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query1})
	time.Sleep(100 * time.Millisecond)

	votes, err := db.GetVotes(database, g.ID)
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 1 || votes[0].Value != "5" {
		t.Fatalf("esperaba 1 voto con valor 5, obtuve %+v", votes)
	}

	query2 := &tgbotapi.CallbackQuery{
		From:    &tgbotapi.User{ID: 1, UserName: "creator"},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 1}},
		Data:    "vote:novote:8",
	}
	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query2})
	time.Sleep(100 * time.Millisecond)

	votes, err = db.GetVotes(database, g.ID)
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 1 || votes[0].Value != "5" {
		t.Fatalf("el voto no debería haber cambiado, obtuve %+v", votes)
	}
}

func TestHandleResetVotesClearsVotes(t *testing.T) {
	b, database, _ := newTestBot(t)

	g, err := db.CreateGame(database, "reset123", 1, "creator")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := db.StartGame(database, g.ID); err != nil {
		t.Fatalf("start game: %v", err)
	}
	if err := db.CastVote(database, g.ID, 1, "creator", "5"); err != nil {
		t.Fatalf("cast vote: %v", err)
	}

	query := &tgbotapi.CallbackQuery{
		From: &tgbotapi.User{ID: 1, UserName: "creator"},
		Message: &tgbotapi.Message{
			Chat:      &tgbotapi.Chat{ID: 1},
			MessageID: 200,
		},
		Data: "reset_votes:reset123",
	}
	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query})
	time.Sleep(100 * time.Millisecond)

	votes, err := db.GetVotes(database, g.ID)
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 0 {
		t.Fatalf("esperaba 0 votos tras relanzar, obtuve %d", len(votes))
	}

	g2, err := db.GetGame(database, g.ID)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	if g2.Status != db.GameStatusVoting {
		t.Fatalf("esperaba estado voting, obtuve %s", g2.Status)
	}
}

func TestHandleResetVotesRejectsNonCreator(t *testing.T) {
	b, database, _ := newTestBot(t)

	g, err := db.CreateGame(database, "reset456", 1, "creator")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := db.StartGame(database, g.ID); err != nil {
		t.Fatalf("start game: %v", err)
	}
	if _, err := db.AddPlayer(database, g.ID, 2, "joiner"); err != nil {
		t.Fatalf("add player: %v", err)
	}
	if err := db.CastVote(database, g.ID, 1, "creator", "5"); err != nil {
		t.Fatalf("cast vote: %v", err)
	}

	query := &tgbotapi.CallbackQuery{
		From: &tgbotapi.User{ID: 2, UserName: "joiner"},
		Message: &tgbotapi.Message{
			Chat:      &tgbotapi.Chat{ID: 2},
			MessageID: 200,
		},
		Data: "reset_votes:reset456",
	}
	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query})
	time.Sleep(100 * time.Millisecond)

	votes, err := db.GetVotes(database, g.ID)
	if err != nil {
		t.Fatalf("get votes: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("el joiner no debería poder borrar votos, obtuve %d votos", len(votes))
	}
}

func TestHandleCreateGameLinkIsCopiable(t *testing.T) {
	b, _, mock := newTestBot(t)

	query := &tgbotapi.CallbackQuery{
		From: &tgbotapi.User{ID: 1, UserName: "creator"},
		Message: &tgbotapi.Message{
			Chat:      &tgbotapi.Chat{ID: 1},
			MessageID: 100,
		},
		Data: "create_game",
	}

	b.HandleUpdate(tgbotapi.Update{CallbackQuery: query})
	time.Sleep(100 * time.Millisecond)

	if mock.requestedCount() == 0 {
		t.Fatal("esperaba que se editara un mensaje")
	}

	edit, ok := mock.requested[0].(tgbotapi.EditMessageTextConfig)
	if !ok {
		t.Fatalf("esperaba EditMessageTextConfig, obtuve %T", mock.requested[0])
	}

	if strings.Contains(edit.Text, "Código") {
		t.Fatal("el mensaje no debería contener la palabra Código")
	}
	if !strings.Contains(edit.Text, "https://t.me/testbot?start=") {
		t.Fatalf("el mensaje debería contener el link completo, obtuve: %s", edit.Text)
	}
}
