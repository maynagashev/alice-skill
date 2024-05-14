package pg

import (
	"context"
	"database/sql"
	"github.com/maynagashev/alice-skill/internal/store"
	"time"
)

// Store реализует интерфейс store.Store и позволяет взаимодействовать с СУБД PostgreSQL
type Store struct {
	// Поле conn содержит объект соединения с СУБД
	conn *sql.DB
}

// NewStore возвращает новый экземпляр PostgreSQL-хранилища
func NewStore(conn *sql.DB) *Store {
	return &Store{conn: conn}
}

// Bootstrap подготавливает БД к работе, создавая необходимые таблицы и индексы
func (s Store) Bootstrap(ctx context.Context) error {
	// запускаем транзакцию
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	// в случае неуспешного коммита все изменения транзакции будут отменены
	defer tx.Rollback()

	// создаём таблицу пользователей и необходимые индексы
	tx.ExecContext(ctx, `
        CREATE TABLE users (
            id varchar(128) PRIMARY KEY,
            username varchar(128)
        )
    `)
	tx.ExecContext(ctx, `CREATE UNIQUE INDEX sender_idx ON users (username)`)

	// создаём таблицу сообщений и необходимые индексы
	tx.ExecContext(ctx, `
        CREATE TABLE messages (
            id serial PRIMARY KEY,
            sender varchar(128),
            recipient varchar(128),
            payload text,
            sent_at timestamp with time zone,
            read_at timestamp with time zone DEFAULT NULL
        )
    `)
	tx.ExecContext(ctx, `CREATE INDEX recipient_idx ON messages (recipient)`)

	// коммитим транзакцию
	return tx.Commit()
}

func (s Store) FindRecipient(ctx context.Context, username string) (userID string, err error) {
	// запрашиваем внутренний идентификатор пользователя по его имени
	row := s.conn.QueryRowContext(ctx, `SELECT id FROM users WHERE username = $1`, username)
	err = row.Scan(&userID)
	return
}

func (s Store) ListMessages(ctx context.Context, userID string) ([]store.Message, error) {
	// запрашиваем данные обо всех сообщениях пользователя, без самого текста
	rows, err := s.conn.QueryContext(ctx, `
        SELECT
            m.id,
            u.username AS sender,
            m.sent_at
        FROM messages m
        JOIN users u ON m.sender = u.id
        WHERE
            m.recipient = $1
    `, userID)

	if err != nil {
		return nil, err
	}
	// не забываем закрыть курсор после завершения работы с данными
	defer rows.Close()

	// считываем записи в слайс сообщений
	var messages []store.Message
	for rows.Next() {
		var m store.Message
		if err := rows.Scan(&m.ID, &m.Sender, &m.Time); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}

	// необходимо проверить ошибки уровня курсора
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

func (s Store) GetMessage(ctx context.Context, id int64) (*store.Message, error) {
	// запрашиваем сообщение по внутреннему идентификатору
	row := s.conn.QueryRowContext(ctx, `
        SELECT
            m.id,
            u.username AS sender,
            m.payload,
            m.sent_at
        FROM messages m
        JOIN users u ON m.sender = u.id
        WHERE
            m.id = $1
    `,
		id,
	)

	// считываем значения из записи БД в соответствующие поля структуры
	var msg store.Message
	err := row.Scan(&msg.ID, &msg.Sender, &msg.Payload, &msg.Time)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s Store) SaveMessage(ctx context.Context, userID string, msg store.Message) error {
	// добавляем новое сообщение в БД
	_, err := s.conn.ExecContext(ctx, `
        INSERT INTO messages
        (sender, recipient, payload, sent_at)
        VALUES
        ($1, $2, $3, $4);
    `, msg.Sender, userID, msg.Payload, time.Now())

	return err
}
