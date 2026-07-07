package bot

import (
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type mockTelegramAPI struct {
	mu        sync.Mutex
	sent      []tgbotapi.Chattable
	requested []tgbotapi.Chattable
}

func (m *mockTelegramAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, c)
	return tgbotapi.Message{}, nil
}

func (m *mockTelegramAPI) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requested = append(m.requested, c)
	return &tgbotapi.APIResponse{}, nil
}

func (m *mockTelegramAPI) sentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func (m *mockTelegramAPI) requestedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requested)
}

func TestSenderQueuesAndDispatchesTextMessage(t *testing.T) {
	mock := &mockTelegramAPI{}
	sender := NewSender(mock)
	sender.Start()

	sender.Queue(OutboxMessage{
		Type:   messageTypeText,
		ChatID: 42,
		Text:   "hola",
	})

	sender.Stop()
	time.Sleep(50 * time.Millisecond)

	if mock.sentCount() != 1 {
		t.Fatalf("esperaba 1 mensaje enviado, obtuve %d", mock.sentCount())
	}
}

func TestSenderDispatchesCallbackAnswer(t *testing.T) {
	mock := &mockTelegramAPI{}
	sender := NewSender(mock)
	sender.Start()

	sender.Queue(OutboxMessage{
		Type:       messageTypeCallback,
		CallbackID: "callback-123",
		Text:       "ok",
	})

	sender.Stop()
	time.Sleep(50 * time.Millisecond)

	if mock.requestedCount() != 1 {
		t.Fatalf("esperaba 1 request, obtuve %d", mock.requestedCount())
	}
}

func TestSenderDispatchesKeyboardAndEdit(t *testing.T) {
	mock := &mockTelegramAPI{}
	sender := NewSender(mock)
	sender.Start()

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("A", "a"),
		),
	)

	sender.Queue(OutboxMessage{
		Type:     messageTypeKeyboard,
		ChatID:   1,
		Text:     "teclado",
		Keyboard: &keyboard,
	})

	sender.Queue(OutboxMessage{
		Type:      messageTypeEdit,
		ChatID:    1,
		Text:      "editado",
		MessageID: 99,
	})

	sender.Stop()
	time.Sleep(50 * time.Millisecond)

	if mock.sentCount() != 1 {
		t.Fatalf("esperaba 1 mensaje enviado, obtuve %d", mock.sentCount())
	}
	if mock.requestedCount() != 1 {
		t.Fatalf("esperaba 1 request, obtuve %d", mock.requestedCount())
	}
}
