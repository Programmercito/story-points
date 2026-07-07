package bot

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramAPI es la superficie mínima de la API de Telegram que usa el bot.
// Permite reemplazar la implementación real por un mock en tests.
type TelegramAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

// OutboxMessage representa un mensaje pendiente de envío.
type OutboxMessage struct {
	Type       string
	ChatID     int64
	Text       string
	Keyboard   *tgbotapi.InlineKeyboardMarkup
	MessageID  int
	CallbackID string
}

const (
	messageTypeText     = "text"
	messageTypeKeyboard = "keyboard"
	messageTypeEdit     = "edit"
	messageTypeCallback = "callback"
)

// Sender encapsula una cola de mensajes salientes y un worker que los envía.
type Sender struct {
	api    TelegramAPI
	outbox chan OutboxMessage
}

// NewSender crea un sender con un buffer de 100 mensajes.
func NewSender(api TelegramAPI) *Sender {
	return &Sender{
		api:    api,
		outbox: make(chan OutboxMessage, 100),
	}
}

// Start lanza el worker que consume la cola en una goroutine.
func (s *Sender) Start() {
	go s.worker()
}

// Stop cierra el canal de salida. No se pueden enviar más mensajes después.
func (s *Sender) Stop() {
	close(s.outbox)
}

// Queue agrega un mensaje a la cola para ser enviado asíncronamente.
func (s *Sender) Queue(msg OutboxMessage) {
	s.outbox <- msg
}

func (s *Sender) worker() {
	for msg := range s.outbox {
		s.dispatch(msg)
	}
}

func (s *Sender) dispatch(msg OutboxMessage) {
	switch msg.Type {
	case messageTypeText:
		m := tgbotapi.NewMessage(msg.ChatID, msg.Text)
		m.ParseMode = tgbotapi.ModeMarkdown
		if _, err := s.api.Send(m); err != nil {
			log.Printf("send text: %v", err)
		}
	case messageTypeKeyboard:
		m := tgbotapi.NewMessage(msg.ChatID, msg.Text)
		m.ParseMode = tgbotapi.ModeMarkdown
		m.ReplyMarkup = *msg.Keyboard
		if _, err := s.api.Send(m); err != nil {
			log.Printf("send inline keyboard: %v", err)
		}
	case messageTypeEdit:
		edit := tgbotapi.NewEditMessageText(msg.ChatID, msg.MessageID, msg.Text)
		edit.ParseMode = tgbotapi.ModeMarkdown
		if msg.Keyboard != nil {
			edit.ReplyMarkup = msg.Keyboard
		}
		if _, err := s.api.Request(edit); err != nil {
			log.Printf("edit message: %v", err)
		}
	case messageTypeCallback:
		if _, err := s.api.Request(tgbotapi.NewCallback(msg.CallbackID, msg.Text)); err != nil {
			log.Printf("answer callback: %v", err)
		}
	}
}
