package bot

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"story-points/internal/db"
	"story-points/internal/game"
)

type Bot struct {
	api        *tgbotapi.BotAPI
	db         *sql.DB
	botUsername string
}

func New(api *tgbotapi.BotAPI, database *sql.DB, botUsername string) *Bot {
	return &Bot{
		api:         api,
		db:          database,
		botUsername: botUsername,
	}
}

func (b *Bot) HandleUpdate(update tgbotapi.Update) {
	if update.Message != nil && update.Message.IsCommand() {
		b.handleCommand(update.Message)
		return
	}
	if update.CallbackQuery != nil {
		b.handleCallback(update.CallbackQuery)
		return
	}
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		b.handleStart(msg)
	case "juegos":
		b.handleJuegos(msg)
	default:
		b.sendText(msg.Chat.ID, "Comando no reconocido. Usá /start para comenzar o /juegos para ver tus partidas.")
	}
}

func (b *Bot) handleStart(msg *tgbotapi.Message) {
	if !hasUsername(msg.From) {
		b.sendText(msg.Chat.ID, usernameRequiredMessage())
		return
	}

	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		b.showMainMenu(msg.Chat.ID)
		return
	}

	b.joinGame(msg.Chat.ID, msg.From, args)
}

func (b *Bot) handleJuegos(msg *tgbotapi.Message) {
	games, err := db.GetGamesByUser(b.db, msg.From.ID)
	if err != nil {
		b.sendText(msg.Chat.ID, "Error al buscar tus juegos.")
		log.Printf("get games by user: %v", err)
		return
	}

	if len(games) == 0 {
		b.sendText(msg.Chat.ID, "No estás en ningún juego activo. Usá /start para crear uno.")
		return
	}

	b.sendGameList(msg.Chat.ID, games)
}

func (b *Bot) sendGameList(chatID int64, games []db.Game) {
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, g := range games {
		label := fmt.Sprintf("%s • @%s • %s", g.ID[:8], g.CreatorUsername, statusLabel(g.Status))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("enter_game:%s", g.ID)),
		))
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.sendInlineKeyboard(chatID, "Tus juegos activos. Tocá uno para entrar:", keyboard)
}

func statusLabel(status db.GameStatus) string {
	switch status {
	case db.GameStatusWaiting:
		return "esperando"
	case db.GameStatusVoting:
		return "votando"
	case db.GameStatusRevealed:
		return "finalizado"
	default:
		return string(status)
	}
}

func (b *Bot) showMainMenu(chatID int64) {
	text := "Bienvenido al votador de story points. Creá un nuevo juego o gestioná las partidas en las que estás."
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Crear nuevo juego", "create_game"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Mis juegos", "my_games"),
		),
	)
	b.sendInlineKeyboard(chatID, text, keyboard)
}

func (b *Bot) joinGame(chatID int64, user *tgbotapi.User, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil {
		b.sendText(chatID, "Error al buscar el juego. Intentá más tarde.")
		log.Printf("get game %s: %v", gameID, err)
		return
	}
	if g == nil {
		b.sendText(chatID, "Ese link de juego no existe o expiró.")
		return
	}

	if g.Status == db.GameStatusRevealed {
		b.sendText(chatID, "Este juego ya terminó. Pedile al organizador que cree uno nuevo.")
		return
	}

	exists, err := db.PlayerExists(b.db, gameID, user.ID)
	if err != nil {
		b.sendText(chatID, "Error al verificar tu participación.")
		log.Printf("player exists: %v", err)
		return
	}

	player, err := db.AddPlayer(b.db, gameID, user.ID, user.UserName)
	if err != nil {
		b.sendText(chatID, "No pudimos unirte al juego.")
		log.Printf("add player: %v", err)
		return
	}

	if !exists {
		b.notifyCreatorPlayerJoined(g, player)
	}

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		log.Printf("get active players: %v", err)
	}

	var text string
	if g.Status == db.GameStatusWaiting {
		text = fmt.Sprintf("Te uniste al juego creado por @%s.\n\nJugadores actuales:\n%s\n\nEsperá a que el organizador inicie la votación.", g.CreatorUsername, formatPlayerList(players))
	} else {
		text = fmt.Sprintf("Volviste al juego de @%s.\n\nJugadores activos:\n%s", g.CreatorUsername, formatPlayerList(players))
		b.sendVotingKeyboard(chatID, g, user.ID)
	}

	b.sendText(chatID, text)
}

func (b *Bot) notifyCreatorPlayerJoined(g *db.Game, p *db.Player) {
	players, err := db.GetActivePlayers(b.db, g.ID)
	if err != nil {
		log.Printf("notify players: %v", err)
		return
	}

	text := fmt.Sprintf("@%s se unió a tu juego.\n\nJugadores activos (%d):\n%s", p.Username, len(players), formatPlayerList(players))

	if g.Status == db.GameStatusWaiting {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Empezar juego", fmt.Sprintf("start_game:%s", g.ID)),
			),
		)
		b.sendInlineKeyboard(g.CreatorID, text, keyboard)
		return
	}

	b.sendText(g.CreatorID, text)
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	defer func() {
		if _, err := b.api.Request(tgbotapi.NewCallback(query.ID, "")); err != nil {
			log.Printf("answer callback: %v", err)
		}
	}()

	parts := strings.SplitN(query.Data, ":", 3)
	action := parts[0]

	switch action {
	case "create_game":
		b.handleCreateGame(query)
	case "start_game":
		b.handleStartGame(query, parts[1])
	case "vote":
		b.handleVote(query, parts[1], parts[2])
	case "reveal":
		b.handleReveal(query, parts[1])
	case "leave":
		b.handleLeave(query, parts[1])
	case "replay":
		b.handleReplay(query, parts[1])
	case "new_game":
		b.handleNewGame(query)
	case "my_games":
		b.handleMyGamesCallback(query)
	case "enter_game":
		b.handleEnterGame(query, parts[1])
	}
}

func (b *Bot) handleCreateGame(query *tgbotapi.CallbackQuery) {
	user := query.From
	gameID, err := game.GenerateID()
	if err != nil {
		log.Printf("generate id: %v", err)
		return
	}

	g, err := db.CreateGame(b.db, gameID, user.ID, user.UserName)
	if err != nil {
		log.Printf("create game: %v", err)
		return
	}

	link := game.DeepLink(b.botUsername, g.ID)
	text := fmt.Sprintf("Nuevo juego creado.\n\nCompartí este link para que se unan:\n%s\n\nCódigo: `%s`", link, g.ID)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Empezar juego", fmt.Sprintf("start_game:%s", g.ID)),
		),
	)

	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, text, &keyboard)
}

func (b *Bot) handleStartGame(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		return
	}
	if query.From.ID != g.CreatorID {
		b.answerCallback(query.ID, "Solo el organizador puede iniciar el juego.")
		return
	}

	if err := db.StartGame(b.db, gameID); err != nil {
		log.Printf("start game: %v", err)
		return
	}

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		log.Printf("get active players: %v", err)
		return
	}

	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, "El juego comenzó. Se enviaron los teclados de votación a cada jugador.", nil)

	for _, p := range players {
		b.sendVotingKeyboard(p.UserID, g, p.UserID)
	}
}

func (b *Bot) sendVotingKeyboard(chatID int64, g *db.Game, userID int64) {
	opts := game.Options()
	var rows [][]tgbotapi.InlineKeyboardButton
	var current []tgbotapi.InlineKeyboardButton
	for i, opt := range opts {
		current = append(current, tgbotapi.NewInlineKeyboardButtonData(opt, fmt.Sprintf("vote:%s:%s", g.ID, opt)))
		if (i+1)%4 == 0 {
			rows = append(rows, current)
			current = nil
		}
	}
	if len(current) > 0 {
		rows = append(rows, current)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Salir del juego", fmt.Sprintf("leave:%s", g.ID)),
		tgbotapi.NewInlineKeyboardButtonData("Mis juegos", "my_games"),
	))

	hasVoted, _ := db.HasVoted(b.db, g.ID, userID)
	text := fmt.Sprintf("Juego de @%s\n\nElegí tu story point:", g.CreatorUsername)
	if hasVoted {
		text += "\n\nYa votaste. Podés cambiar tu voto presionando otra opción."
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.sendInlineKeyboard(chatID, text, keyboard)
}

func (b *Bot) handleVote(query *tgbotapi.CallbackQuery, gameID, value string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		return
	}
	if g.Status != db.GameStatusVoting {
		b.answerCallback(query.ID, "La votación ya no está abierta.")
		return
	}

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		return
	}
	if !isPlayerActive(players, query.From.ID) {
		b.answerCallback(query.ID, "No sos parte activa de este juego.")
		return
	}

	if err := db.CastVote(b.db, gameID, query.From.ID, query.From.UserName, value); err != nil {
		log.Printf("cast vote: %v", err)
		return
	}

	b.answerCallback(query.ID, fmt.Sprintf("Votaste %s", value))

	votes, err := db.GetVotes(b.db, gameID)
	if err != nil {
		return
	}

	progress := game.VotingProgress(players, votes)
	b.sendText(g.CreatorID, fmt.Sprintf("Progreso de votación:\n\n%s", progress))

	if len(votes) == len(players) {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Revelar votos", fmt.Sprintf("reveal:%s", g.ID)),
			),
		)
		b.sendInlineKeyboard(g.CreatorID, "¡Todos los jugadores votaron! Podés revelar los resultados.", keyboard)
	}
}

func (b *Bot) handleReveal(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		return
	}
	if query.From.ID != g.CreatorID {
		b.answerCallback(query.ID, "Solo el organizador puede revelar los votos.")
		return
	}
	if g.Status != db.GameStatusVoting {
		b.answerCallback(query.ID, "La votación no está activa.")
		return
	}

	if err := db.RevealVotes(b.db, gameID); err != nil {
		log.Printf("reveal votes: %v", err)
		return
	}

	votes, err := db.GetVotes(b.db, gameID)
	if err != nil {
		log.Printf("get votes: %v", err)
		return
	}

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		log.Printf("get active players: %v", err)
		return
	}

	result := game.FormatVotes(votes)
	text := fmt.Sprintf("Resultados del juego de @%s:\n\n%s", g.CreatorUsername, result)

	for _, p := range players {
		b.sendText(p.UserID, text)
	}

	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, text, nil)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Jugar de nuevo con los mismos", fmt.Sprintf("replay:%s", g.ID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Nuevo juego", "new_game"),
		),
	)
	b.sendInlineKeyboard(g.CreatorID, "¿Querés jugar de nuevo?", keyboard)
}

func (b *Bot) handleLeave(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		return
	}

	if err := db.LeaveGame(b.db, gameID, query.From.ID); err != nil {
		log.Printf("leave game: %v", err)
		return
	}

	b.answerCallback(query.ID, "Te saliste del juego.")
	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, "Te saliste del juego. Podés volver a entrar con el link si todavía está abierto.", nil)

	if query.From.ID == g.CreatorID {
		b.sendText(g.CreatorID, "Abandonaste tu propio juego. El juego queda sin organizador.")
		return
	}

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		return
	}
	b.sendText(g.CreatorID, fmt.Sprintf("@%s salió del juego.\n\nJugadores activos (%d):\n%s", query.From.UserName, len(players), formatPlayerList(players)))
}

func (b *Bot) handleReplay(query *tgbotapi.CallbackQuery, oldGameID string) {
	oldGame, err := db.GetGame(b.db, oldGameID)
	if err != nil || oldGame == nil {
		return
	}
	if query.From.ID != oldGame.CreatorID {
		b.answerCallback(query.ID, "Solo el organizador puede reiniciar el juego.")
		return
	}

	newGameID, err := game.GenerateID()
	if err != nil {
		log.Printf("generate id: %v", err)
		return
	}

	g, err := db.CreateGame(b.db, newGameID, oldGame.CreatorID, oldGame.CreatorUsername)
	if err != nil {
		log.Printf("create replay game: %v", err)
		return
	}

	oldPlayers, err := db.GetActivePlayers(b.db, oldGameID)
	if err != nil {
		log.Printf("get old players: %v", err)
		return
	}

	for _, p := range oldPlayers {
		if p.UserID == g.CreatorID {
			continue
		}
		if _, err := db.AddPlayer(b.db, g.ID, p.UserID, p.Username); err != nil {
			log.Printf("copy player: %v", err)
		}
	}

	link := game.DeepLink(b.botUsername, g.ID)
	text := fmt.Sprintf("Nueva ronda creada.\n\nLink para unirse:\n%s\n\nCódigo: `%s`", link, g.ID)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Empezar juego", fmt.Sprintf("start_game:%s", g.ID)),
		),
	)

	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, text, &keyboard)

	for _, p := range oldPlayers {
		if p.UserID == g.CreatorID {
			continue
		}
		b.sendText(p.UserID, fmt.Sprintf("@%s inició una nueva ronda. Unite con este link:\n%s", g.CreatorUsername, link))
	}
}

func (b *Bot) handleNewGame(query *tgbotapi.CallbackQuery) {
	b.handleCreateGame(query)
}

func (b *Bot) handleMyGamesCallback(query *tgbotapi.CallbackQuery) {
	games, err := db.GetGamesByUser(b.db, query.From.ID)
	if err != nil {
		b.answerCallback(query.ID, "Error al buscar tus juegos.")
		log.Printf("get games by user: %v", err)
		return
	}

	if len(games) == 0 {
		b.answerCallback(query.ID, "No tenés juegos activos.")
		return
	}

	b.sendGameList(query.Message.Chat.ID, games)
}

func (b *Bot) handleEnterGame(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		b.answerCallback(query.ID, "Juego no encontrado.")
		return
	}

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		b.answerCallback(query.ID, "Error al cargar el juego.")
		return
	}
	if !isPlayerActive(players, query.From.ID) {
		b.answerCallback(query.ID, "No sos parte activa de este juego.")
		return
	}

	b.answerCallback(query.ID, "Entrando al juego...")
	b.renderGameMenu(query.Message.Chat.ID, g, query.From.ID)
}

func (b *Bot) renderGameMenu(chatID int64, g *db.Game, userID int64) {
	players, err := db.GetActivePlayers(b.db, g.ID)
	if err != nil {
		log.Printf("render game menu: %v", err)
		return
	}

	switch g.Status {
	case db.GameStatusWaiting:
		link := game.DeepLink(b.botUsername, g.ID)
		text := fmt.Sprintf("Juego de @%s\n\nEstado: esperando jugadores\nLink: %s\n\nJugadores (%d):\n%s", g.CreatorUsername, link, len(players), formatPlayerList(players))
		var rows [][]tgbotapi.InlineKeyboardButton
		if userID == g.CreatorID {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Empezar juego", fmt.Sprintf("start_game:%s", g.ID)),
			))
		}
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Mis juegos", "my_games"),
			),
		)
		b.sendInlineKeyboard(chatID, text, tgbotapi.NewInlineKeyboardMarkup(rows...))

	case db.GameStatusVoting:
		b.sendVotingKeyboard(chatID, g, userID)

	case db.GameStatusRevealed:
		votes, err := db.GetVotes(b.db, g.ID)
		if err != nil {
			log.Printf("get votes: %v", err)
			return
		}
		result := game.FormatVotes(votes)
		text := fmt.Sprintf("Juego de @%s — resultados finales:\n\n%s", g.CreatorUsername, result)
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Jugar de nuevo con los mismos", fmt.Sprintf("replay:%s", g.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Nuevo juego", "new_game"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Mis juegos", "my_games"),
			),
		)
		b.sendInlineKeyboard(chatID, text, keyboard)
	}
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send text: %v", err)
	}
}

func (b *Bot) sendInlineKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send inline keyboard: %v", err)
	}
}

func (b *Bot) editMessage(chatID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeMarkdown
	if keyboard != nil {
		edit.ReplyMarkup = keyboard
	}
	if _, err := b.api.Request(edit); err != nil {
		log.Printf("edit message: %v", err)
	}
}

func (b *Bot) answerCallback(callbackID, text string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(callbackID, text)); err != nil {
		log.Printf("answer callback: %v", err)
	}
}

func hasUsername(user *tgbotapi.User) bool {
	return user != nil && user.UserName != ""
}

func usernameRequiredMessage() string {
	return `Para usar este bot necesitás un nombre de usuario de Telegram.

1. Andá a Configuración → Editar perfil.
2. Elegí un nombre de usuario (@usuario).
3. Volvé a tocar acá: /start`
}

func formatPlayerList(players []db.Player) string {
	var b strings.Builder
	for _, p := range players {
		fmt.Fprintf(&b, "- @%s\n", p.Username)
	}
	return b.String()
}

func isPlayerActive(players []db.Player, userID int64) bool {
	for _, p := range players {
		if p.UserID == userID && p.Status == db.PlayerStatusActive {
			return true
		}
	}
	return false
}
