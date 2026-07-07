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
	api         TelegramAPI
	db          *sql.DB
	sender      *Sender
	botUsername string
}

func New(api TelegramAPI, database *sql.DB, sender *Sender, botUsername string) *Bot {
	return &Bot{
		api:         api,
		db:          database,
		sender:      sender,
		botUsername: botUsername,
	}
}

func (b *Bot) HandleUpdate(update tgbotapi.Update) {
	if update.Message != nil && update.Message.IsCommand() {
		b.handleCommand(update.Message)
		return
	}
	if update.Message != nil {
		b.showMainMenu(update.Message.Chat.ID)
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
	case "menu":
		b.showMainMenu(msg.Chat.ID)
	default:
		b.sendText(msg.Chat.ID, "Comando no reconocido. Usá /start, /menu o /juegos.")
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

	b.sendGameList(msg.Chat.ID, msg.From.ID, games)
}

func (b *Bot) sendGameList(chatID int64, userID int64, games []db.Game) {
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, g := range games {
		label := fmt.Sprintf("%s • @%s • %s", g.ID[:8], g.CreatorUsername, statusLabel(g.Status))
		var actionLabel, actionCallback string
		if userID == g.CreatorID {
			actionLabel = "Eliminar"
			actionCallback = fmt.Sprintf("leave:%s", g.ID)
		} else if g.Status == db.GameStatusRevealed {
			actionLabel = "Eliminar"
			actionCallback = fmt.Sprintf("delete_game:%s", g.ID)
		} else {
			actionLabel = "Salir"
			actionCallback = fmt.Sprintf("leave:%s", g.ID)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("enter_game:%s", g.ID)),
			tgbotapi.NewInlineKeyboardButtonData(actionLabel, actionCallback),
		))
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.sendInlineKeyboard(chatID, "Tus juegos. Tocá uno para entrar, o usá la acción de la derecha para gestionarlo:", keyboard)
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
		if exists {
			text = fmt.Sprintf("Volviste al juego de @%s.\n\nJugadores activos:\n%s", g.CreatorUsername, formatPlayerList(players))
		} else {
			text = fmt.Sprintf("Te uniste al juego de @%s que ya está votando.\n\nJugadores activos:\n%s\n\nElegí tu story point:", g.CreatorUsername, formatPlayerList(players))
		}
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

	if g.Status == db.GameStatusVoting {
		votes, err := db.GetVotes(b.db, g.ID)
		if err == nil {
			progress := game.VotingProgress(players, votes)
			b.sendText(g.CreatorID, fmt.Sprintf("%s\n\n%s", text, progress))
			if len(votes) == len(players) {
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Revelar votos", fmt.Sprintf("reveal:%s", g.ID)),
					),
				)
				b.sendInlineKeyboard(g.CreatorID, "¡Todos los jugadores votaron! Podés revelar los resultados.", keyboard)
			}
			return
		}
	}

	b.sendText(g.CreatorID, text)
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	defer func() {
		b.answerCallback(query.ID, "")
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
	case "reset_votes":
		b.handleResetVotes(query, parts[1])
	case "leave":
		b.handleLeave(query, parts[1])
	case "delete_game":
		b.handleDeleteGame(query, parts[1])
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
	text := fmt.Sprintf("Nuevo juego creado.\n\nComparte este link para que se unan:\n%s", link)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Abrir link para unirse", link),
		),
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

	link := game.DeepLink(b.botUsername, g.ID)
	editText := fmt.Sprintf("El juego comenzó. Se enviaron los teclados de votación a cada jugador.\n\nLink para nuevos participantes:\n%s", link)
	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, editText, nil)

	for _, p := range players {
		b.sendVotingKeyboard(p.UserID, g, p.UserID)
	}
}

func (b *Bot) votingKeyboard(g *db.Game, userID int64) (string, tgbotapi.InlineKeyboardMarkup) {
	opts := game.Options()
	hasVoted, _ := db.HasVoted(b.db, g.ID, userID)

	var rows [][]tgbotapi.InlineKeyboardButton
	if !hasVoted {
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
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Salir del juego", fmt.Sprintf("leave:%s", g.ID)),
		tgbotapi.NewInlineKeyboardButtonData("Mis juegos", "my_games"),
	))

	text := fmt.Sprintf("Juego de @%s\n\nElegí tu story point:", g.CreatorUsername)
	if hasVoted {
		text = fmt.Sprintf("Juego de @%s\n\nYa votaste. Esperá a que el organizador revele o relance la votación.", g.CreatorUsername)
	}

	return text, tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (b *Bot) sendVotingKeyboard(chatID int64, g *db.Game, userID int64) {
	text, keyboard := b.votingKeyboard(g, userID)
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

	hasVoted, err := db.HasVoted(b.db, gameID, query.From.ID)
	if err != nil {
		return
	}
	if hasVoted {
		b.answerCallback(query.ID, "Ya votaste. No podés cambiar tu voto.")
		return
	}

	if err := db.CastVote(b.db, gameID, query.From.ID, query.From.UserName, value); err != nil {
		log.Printf("cast vote: %v", err)
		return
	}

	b.answerCallback(query.ID, fmt.Sprintf("Votaste %s", value))

	// Actualizar el mensaje del jugador para que no pueda votar de nuevo.
	if query.Message != nil {
		text, keyboard := b.votingKeyboard(g, query.From.ID)
		b.editMessage(query.Message.Chat.ID, query.Message.MessageID, text, &keyboard)
	}

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
				tgbotapi.NewInlineKeyboardButtonData("Relanzar votación", fmt.Sprintf("reset_votes:%s", g.ID)),
			),
		)
		b.sendInlineKeyboard(g.CreatorID, "¡Todos los jugadores votaron! Podés revelar los resultados o relanzar la votación.", keyboard)
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

func (b *Bot) handleResetVotes(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		return
	}
	if query.From.ID != g.CreatorID {
		b.answerCallback(query.ID, "Solo el organizador puede relanzar la votación.")
		return
	}
	if g.Status != db.GameStatusVoting {
		b.answerCallback(query.ID, "La votación no está activa.")
		return
	}

	if err := db.DeleteVotes(b.db, gameID); err != nil {
		log.Printf("delete votes: %v", err)
		return
	}

	b.answerCallback(query.ID, "Votación relanzada.")

	players, err := db.GetActivePlayers(b.db, gameID)
	if err != nil {
		log.Printf("get active players: %v", err)
		return
	}

	for _, p := range players {
		b.sendVotingKeyboard(p.UserID, g, p.UserID)
	}

	progress := game.VotingProgress(players, nil)
	b.sendText(g.CreatorID, fmt.Sprintf("Relanzaste la votación.\n\n%s", progress))
}

func (b *Bot) handleDeleteGame(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		b.answerCallback(query.ID, "Juego no encontrado.")
		return
	}

	if err := db.LeaveGame(b.db, gameID, query.From.ID); err != nil {
		b.answerCallback(query.ID, "No se pudo eliminar el juego de tu lista.")
		log.Printf("delete game from list: %v", err)
		return
	}

	b.answerCallback(query.ID, "Juego eliminado de tu lista.")

	games, err := db.GetGamesByUser(b.db, query.From.ID)
	if err != nil {
		return
	}
	if len(games) == 0 {
		b.editMessage(query.Message.Chat.ID, query.Message.MessageID, "No tenés más juegos activos. Usá /start para crear uno.", nil)
		return
	}
	b.sendGameList(query.Message.Chat.ID, query.From.ID, games)
}

func (b *Bot) handleLeave(query *tgbotapi.CallbackQuery, gameID string) {
	g, err := db.GetGame(b.db, gameID)
	if err != nil || g == nil {
		return
	}

	if query.From.ID == g.CreatorID {
		players, err := db.GetActivePlayers(b.db, gameID)
		if err != nil {
			log.Printf("get active players: %v", err)
			return
		}

		if err := db.DeleteGame(b.db, gameID); err != nil {
			b.answerCallback(query.ID, "No se pudo eliminar el juego.")
			log.Printf("delete game: %v", err)
			return
		}

		b.answerCallback(query.ID, "Eliminaste el juego.")
		b.editMessage(query.Message.Chat.ID, query.Message.MessageID, "Eliminaste el juego. Ya no aparece en tu lista ni en la de los demás participantes.", nil)

		for _, p := range players {
			if p.UserID == g.CreatorID {
				continue
			}
			b.sendText(p.UserID, fmt.Sprintf("El organizador @%s eliminó el juego. Ya no está disponible.", g.CreatorUsername))
		}
		return
	}

	if err := db.LeaveGame(b.db, gameID, query.From.ID); err != nil {
		log.Printf("leave game: %v", err)
		return
	}

	b.answerCallback(query.ID, "Te saliste del juego.")
	b.editMessage(query.Message.Chat.ID, query.Message.MessageID, "Te saliste del juego. Podés volver a entrar con el link si todavía está abierto.", nil)

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
	text := fmt.Sprintf("Nueva ronda creada.\n\nLink para unirse:\n%s", link)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Abrir link para unirse", link),
		),
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

	b.sendGameList(query.Message.Chat.ID, query.From.ID, games)
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
		text := fmt.Sprintf("Juego de @%s\n\nEstado: esperando jugadores\n\nLink para unirse:\n%s\n\nJugadores (%d):\n%s", g.CreatorUsername, link, len(players), formatPlayerList(players))
		var rows [][]tgbotapi.InlineKeyboardButton
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Abrir link para unirse", link),
		))
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
		if userID == g.CreatorID {
			votes, err := db.GetVotes(b.db, g.ID)
			if err == nil {
				progress := game.VotingProgress(players, votes)
				b.sendText(chatID, fmt.Sprintf("Estado de la votación de tu juego:\n\n%s", progress))

				var rows [][]tgbotapi.InlineKeyboardButton
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Relanzar votación", fmt.Sprintf("reset_votes:%s", g.ID)),
				))
				if len(votes) == len(players) {
					rows = append(rows, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Revelar votos", fmt.Sprintf("reveal:%s", g.ID)),
					))
					b.sendInlineKeyboard(chatID, "¡Todos votaron! Podés revelar los resultados o relanzar la votación.", tgbotapi.NewInlineKeyboardMarkup(rows...))
				} else {
					b.sendInlineKeyboard(chatID, "Podés relanzar la votación cuando quieras.", tgbotapi.NewInlineKeyboardMarkup(rows...))
				}
			}
		}
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
	b.sender.Queue(OutboxMessage{
		Type:   messageTypeText,
		ChatID: chatID,
		Text:   text,
	})
}

func (b *Bot) sendInlineKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	b.sender.Queue(OutboxMessage{
		Type:     messageTypeKeyboard,
		ChatID:   chatID,
		Text:     text,
		Keyboard: &keyboard,
	})
}

func (b *Bot) editMessage(chatID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	b.sender.Queue(OutboxMessage{
		Type:      messageTypeEdit,
		ChatID:    chatID,
		Text:      text,
		MessageID: messageID,
		Keyboard:  keyboard,
	})
}

func (b *Bot) answerCallback(callbackID, text string) {
	b.sender.Queue(OutboxMessage{
		Type:       messageTypeCallback,
		CallbackID: callbackID,
		Text:       text,
	})
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
