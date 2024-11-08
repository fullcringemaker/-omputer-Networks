package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
	_ "github.com/go-sql-driver/mysql"
	"github.com/rainycape/unidecode"
)

// Конфигурация базы данных
const (
	dbUser     = "iu9networkslabs"
	dbPassword = "Je2dTYr6"
	dbHost     = "students.yss.su"
	dbName     = "iu9networkslabs"
)

// Структура новости
type NewsItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	PubDate     string `json:"pub_date"`
}

// Глобальные переменные для управления подключениями WebSocket
var (
	upgrader    = websocket.Upgrader{}
	clients     = make(map[*websocket.Conn]bool)
	clientsLock = sync.Mutex{}
)

// Главная функция
func main() {
	// Подключение к базе данных
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		dbUser, dbPassword, dbHost, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("Ошибка подключения к базе данных:", err)
	}
	defer db.Close()

	// Проверка соединения
	err = db.Ping()
	if err != nil {
		log.Fatal("Не удалось подключиться к базе данных:", err)
	}

	// Создание таблицы, если она не существует
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS iu9Trofimenko (
	  id INT(11) NOT NULL AUTO_INCREMENT PRIMARY KEY,
	  title TEXT COLLATE 'utf8mb4_unicode_ci' NULL,
	  link TEXT COLLATE 'utf8mb4_unicode_ci' NULL,
	  description TEXT COLLATE 'utf8mb4_unicode_ci' NULL,
	  pub_date DATETIME NULL
	) ENGINE='InnoDB' COLLATE='utf8mb4_unicode_ci';
	`
	_, err = db.Exec(createTableQuery)
	if err != nil {
		log.Fatal("Ошибка создания таблицы:", err)
	}

	// Парсинг RSS и обновление базы данных
	err = parseAndUpdateRSS(db)
	if err != nil {
		log.Println("Ошибка при парсинге RSS:", err)
	}

	// Настройка обработчиков HTTP
	http.HandleFunc("/dashboard", dashboardHandler)
	http.HandleFunc("/parser", parserHandler)
	http.HandleFunc("/ws", handleWebSocket(db))

	// Статические файлы
	http.Handle("/", http.FileServer(http.Dir("./")))

	// Запуск горутины для периодического обновления
	go func() {
		for {
			time.Sleep(30 * time.Second) // Интервал обновления
			err := parseAndUpdateRSS(db)
			if err != nil {
				log.Println("Ошибка при парсинге RSS:", err)
			}
			// Отправка обновленных данных клиентам
			notifyClients(db)
		}
	}()

	// Запуск сервера
	log.Println("Сервер запущен на порту 9742")
	err = http.ListenAndServe(":9742", nil)
	if err != nil {
		log.Fatal("Ошибка запуска сервера:", err)
	}
}

// Обработчик для dashboard.html
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "dashboard.html")
}

// Обработчик для parser.html
func parserHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "parser.html")
}

// Функция обработки WebSocket подключений
func handleWebSocket(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Обновление заголовков для WebSocket
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }

		// Апгрейд соединения
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Ошибка апгрейда:", err)
			return
		}
		defer conn.Close()

		// Регистрация клиента
		clientsLock.Lock()
		clients[conn] = true
		clientsLock.Unlock()

		// Отправка текущих данных
		sendNews(conn, db)

		// Ожидание закрытия соединения
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				clientsLock.Lock()
				delete(clients, conn)
				clientsLock.Unlock()
				break
			}
		}
	}
}

// Функция парсинга RSS и обновления базы данных
func parseAndUpdateRSS(db *sql.DB) error {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://ldpr.ru/rss")
	if err != nil {
		return err
	}

	for _, item := range feed.Items {
		title := unidecode.Unidecode(item.Title)
		link := item.Link
		description := unidecode.Unidecode(item.Description)
		pubDate, err := item.PublishedParsed, error(nil)
		if item.PublishedParsed == nil {
			pubDate = new(time.Time)
			*pubDate = time.Now()
		}

		// Проверка на существование записи
		var exists bool
		checkQuery := "SELECT EXISTS(SELECT 1 FROM iu9Trofimenko WHERE link = ?)"
		err = db.QueryRow(checkQuery, link).Scan(&exists)
		if err != nil {
			log.Println("Ошибка проверки существования записи:", err)
			continue
		}

		// Вставка новой записи, если ее нет
		if !exists {
			insertQuery := "INSERT INTO iu9Trofimenko (title, link, description, pub_date) VALUES (?, ?, ?, ?)"
			_, err := db.Exec(insertQuery, title, link, description, pubDate)
			if err != nil {
				log.Println("Ошибка вставки записи:", err)
				continue
			}
			log.Println("Добавлена новая новость:", title)
		}
	}

	return nil
}

// Функция получения всех новостей из базы данных
func getAllNews(db *sql.DB) ([]NewsItem, error) {
	rows, err := db.Query("SELECT id, title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var news []NewsItem
	for rows.Next() {
		var item NewsItem
		var pubDate time.Time
		err := rows.Scan(&item.ID, &item.Title, &item.Link, &item.Description, &pubDate)
		if err != nil {
			return nil, err
		}
		item.PubDate = pubDate.Format("02.01.2006")
		news = append(news, item)
	}
	return news, nil
}

// Функция отправки новостей конкретному клиенту
func sendNews(conn *websocket.Conn, db *sql.DB) {
	news, err := getAllNews(db)
	if err != nil {
		log.Println("Ошибка получения новостей:", err)
		return
	}
	data, err := json.Marshal(news)
	if err != nil {
		log.Println("Ошибка маршалинга данных:", err)
		return
	}
	err = conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.Println("Ошибка отправки данных клиенту:", err)
	}
}

// Функция уведомления всех клиентов об обновлении
func notifyClients(db *sql.DB) {
	clientsLock.Lock()
	defer clientsLock.Unlock()

	for conn := range clients {
		sendNews(conn, db)
	}
}
