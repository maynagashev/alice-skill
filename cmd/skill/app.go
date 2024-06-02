package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maynagashev/alice-skill/internal/logger"
	"github.com/maynagashev/alice-skill/internal/models"
	"github.com/maynagashev/alice-skill/internal/store"
	"go.uber.org/zap"
)

// app инкапсулирует в себя все зависимости и логику приложения
type app struct {
	store store.Store

	// канал для отложенной отправки новых сообщений
	msgChan chan store.Message
}

func newApp(s store.Store) *app {
	instance := &app{
		store:   s,
		msgChan: make(chan store.Message, 1024), // установим каналу буфер в 1024 сообщения
	}

	// запустим горутину с фоновым сохранением новых сообщений
	go instance.flushMessages()

	return instance
}

func (a *app) webhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		logger.Log.Debug("got request with bad method", zap.String("method", r.Method))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	logger.Log.Debug("decoding request")
	var req models.Request
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		logger.Log.Debug("cannot decode request JSON body", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if req.Request.Type != models.TypeSimpleUtterance {
		logger.Log.Debug("unsupported request type", zap.String("type", req.Request.Type))
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	// текст ответа навыка
	var text string

	switch true {
	// пользователь попросил отправить сообщение
	case strings.HasPrefix(req.Request.Command, "Отправь"):
		// гипотетическая функция parseSendCommand вычленит из запроса логин адресата и текст сообщения
		username, message := parseSendCommand(req.Request.Command)

		// найдём внутренний идентификатор адресата по его логину
		recipientID, err := a.store.FindRecipient(ctx, username)
		if err != nil {
			logger.Log.Debug("cannot find recipient by username", zap.String("username", username), zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// отправим сообщение в очередь на сохранение
		a.msgChan <- store.Message{
			Sender:    req.Session.User.UserID,
			Recipient: recipientID,
			Time:      time.Now(),
			Payload:   message,
		}

		// оповестим отправителя об успешности операции
		text = "Сообщение успешно отправлено"

	// пользователь попросил прочитать сообщение
	case strings.HasPrefix(req.Request.Command, "Прочитай"):
		// гипотетическая функция parseReadCommand вычленит из запроса порядковый номер сообщения в списке доступных
		messageIndex := parseReadCommand(req.Request.Command)

		// получим список непрослушанных сообщений пользователя
		messages, err := a.store.ListMessages(ctx, req.Session.User.UserID)
		if err != nil {
			logger.Log.Debug("cannot load messages for user", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		text = "Для вас нет новых сообщений."
		if len(messages) < messageIndex {
			// пользователь попросил прочитать сообщение, которого нет
			text = "Такого сообщения не существует."
		} else {
			// получим сообщение по идентификатору
			messageID := messages[messageIndex].ID
			message, err := a.store.GetMessage(ctx, messageID)
			if err != nil {
				logger.Log.Debug("cannot load message", zap.Int64("id", messageID), zap.Error(err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// передадим текст сообщения в ответе
			text = fmt.Sprintf("Сообщение от %s, отправлено %s: %s", message.Sender, message.Time, message.Payload)
		}
		// пользователь хочет зарегистрироваться
	case strings.HasPrefix(req.Request.Command, "Зарегистрируй"):
		// гипотетическая функция parseRegisterCommand вычленит из запроса
		// желаемое имя нового пользователя
		username := parseRegisterCommand(req.Request.Command)

		// регистрируем пользователя
		err := a.store.RegisterUser(ctx, req.Session.User.UserID, username)
		// наличие неспецифичной ошибки
		if err != nil && !errors.Is(err, store.ErrConflict) {
			logger.Log.Debug("cannot register user", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// определяем правильное ответное сообщение пользователю
		text = fmt.Sprintf("Вы успешно зарегистрированы под именем %s", username)
		if errors.Is(err, store.ErrConflict) {
			// ошибка специфична для случая конфликта имён пользователей
			text = "Извините, такое имя уже занято. Попробуйте другое."
		}

	// если не поняли команду, просто скажем пользователю, сколько у него новых сообщений
	default:
		messages, err := a.store.ListMessages(ctx, req.Session.User.UserID)
		if err != nil {
			logger.Log.Debug("cannot load messages for user", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		text = "Для вас нет новых сообщений."
		if len(messages) > 0 {
			text = fmt.Sprintf("Для вас %d новых сообщений.", len(messages))
		}

		// первый запрос новой сессии
		if req.Session.New {
			// обработаем поле Timezone запроса
			tz, err := time.LoadLocation(req.Timezone)
			if err != nil {
				logger.Log.Debug("cannot parse timezone")
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// получим текущее время в часовом поясе пользователя
			now := time.Now().In(tz)
			hour, minute, _ := now.Clock()

			// формируем новый текст приветствия
			text = fmt.Sprintf("Точное время %d часов, %d минут. %s", hour, minute, text)
		}
	}

	// заполним модель ответа
	resp := models.Response{
		Response: models.ResponsePayload{
			Text: text, // Алиса проговорит текст
		},
		Version: "1.0",
	}

	w.Header().Set("Content-Type", "application/json")

	// сериализуем ответ сервера
	enc := json.NewEncoder(w)
	if err := enc.Encode(resp); err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))
		return
	}
	logger.Log.Debug("sending HTTP 200 response")
}

// flushMessages постоянно сохраняет несколько сообщений в хранилище с определённым интервалом
func (a *app) flushMessages() {
	// будем сохранять сообщения, накопленные за последние 10 секунд
	ticker := time.NewTicker(10 * time.Second)

	var messages []store.Message

	for {
		select {
		case msg := <-a.msgChan:
			// добавим сообщение в слайс для последующего сохранения
			messages = append(messages, msg)
		case <-ticker.C:
			// подождём, пока придёт хотя бы одно сообщение
			if len(messages) == 0 {
				continue
			}
			// сохраним все пришедшие сообщения одновременно
			err := a.store.SaveMessages(context.TODO(), messages...)
			if err != nil {
				logger.Log.Debug("cannot save messages", zap.Error(err))
				// не будем стирать сообщения, попробуем отправить их чуть позже
				continue
			}
			// сотрём успешно отосланные сообщения
			messages = nil
		}
	}
}

func parseRegisterCommand(command string) string {
	return "username"
}

func parseSendCommand(command string) (string, string) {
	return "username", "message"
}

func parseReadCommand(command string) int {
	return 1
}
