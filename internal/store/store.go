package store

import (
	"context"
	"time"
)

// Store описывает абстрактное хранилище сообщений пользователей
type Store interface {
	// FindRecipient возвращает внутренний идентификатор пользователя по человекопонятному имени
	FindRecipient(ctx context.Context, username string) (userID string, err error)
	// ListMessages возвращает список всех сообщений для определённого получателя
	ListMessages(ctx context.Context, userID string) ([]Message, error)
	// GetMessage возвращает сообщение с определённым ID
	GetMessage(ctx context.Context, id int64) (*Message, error)
	// SaveMessage сохраняет новое сообщение
	SaveMessage(ctx context.Context, userID string, msg Message) error
}

// Message описывает объект сообщения
type Message struct {
	ID      int64     // внутренний идентификатор сообщения
	Sender  string    // отправитель
	Time    time.Time // время отправления
	Payload string    // текст сообщения
}
