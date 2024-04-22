// пакеты исполняемых приложений должны называться main
package main

import (
	"github.com/maynagashev/alice-skill/internal/logger"
	"go.uber.org/zap"
	"net/http"
)

// Функция main вызывается автоматически при запуске приложения
func main() {
	parseFlags()

	if err := run(); err != nil {
		panic(err)
	}
}

// Функция run будет полезна при инициализации зависимостей сервера перед запуском
func run() error {
	if err := logger.Initialize(flagLogLevel); err != nil {
		return err
	}

	logger.Log.Info("Running server", zap.String("address", flagRunAddr))
	return http.ListenAndServe(flagRunAddr, logger.RequestLogger(webhook))
}

// Функция webhook — обработчик HTTP-запроса
func webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Log.Debug("got request with bad method", zap.String("method", r.Method))
		// разрешаем только POST-запросы
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// установим правильный заголовок для типа данных
	w.Header().Set("Content-Type", "application/json")
	// пока установим ответ-заглушку, без проверки ошибок
	_, _ = w.Write([]byte(`
      {
        "response": {
          "text": "Извините, я пока ничего не умею"
        },
        "version": "1.0"
      }
    `))

	logger.Log.Debug("sending HTTP 200 response")
}
