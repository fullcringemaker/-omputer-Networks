package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
	_ "github.com/go-sql-driver/mysql"
	"github.com/rainycape/unidecode"
)

// NewsItem представляет структуру новости
type NewsItem struct {
	ID          int     `json:"id"`
	Title       *string `json:"title,omitempty"`
	Link        *string `json:"link,omitempty"`
	Description *string `json:"description,omitempty"`
	PubDate     *string `json:"pub_date,omitempty"`
}

// WebSocketHub управляет подключениями WebSocket
type WebSocketHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []NewsItem
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
}

func newHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []NewsItem),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *WebSocketHub) run() {
	for {
		select {
		case conn := <-h.register:
			h.clients[conn] = true
		case conn := <-h.unregister:
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
		case news := <-h.broadcast:
			message, err := json.Marshal(news)
			if err != nil {
				log.Println("Ошибка маршалинга:", err)
				continue
			}
			for conn := range h.clients {
				err := conn.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Println("Ошибка отправки сообщения:", err)
					conn.Close()
					delete(h.clients, conn)
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	// Настройки базы данных
	dbUser := "iu9networkslabs"
	dbPassword := "Je2dTYr6"
	dbName := "iu9networkslabs"
	dbHost := "students.yss.su"

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", dbUser, dbPassword, dbHost, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("Ошибка подключения к базе данных:", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatal("База данных недоступна:", err)
	}

	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(hub, db, w, r)
	})

	http.HandleFunc("/dashboard.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "dashboard.html")
	})

	http.HandleFunc("/parser.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "parser.html")
	})

	// Парсинг RSS и обновление базы данных
	go func() {
		for {
			err := parseAndUpdate(db, hub)
			if err != nil {
				log.Println("Ошибка при парсинге или обновлении:", err)
			}
			time.Sleep(5 * time.Minute) // Периодичность обновления
		}
	}()

	// Проверка на удаление всех записей и восстановление через 1 минуту
	go func() {
		for {
			time.Sleep(30 * time.Second)
			count := 0
			err := db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko").Scan(&count)
			if err != nil {
				log.Println("Ошибка подсчета записей:", err)
				continue
			}
			if count == 0 {
				log.Println("Все записи удалены. Восстановление через 1 минуту.")
				time.AfterFunc(1*time.Minute, func() {
					err := parseAndUpdate(db, hub)
					if err != nil {
						log.Println("Ошибка восстановления данных:", err)
					}
				})
			}
		}
	}()

	log.Println("Сервер запущен на порту 9742")
	err = http.ListenAndServe(":9742", nil)
	if err != nil {
		log.Fatal("Ошибка запуска сервера:", err)
	}
}

func handleWebSocket(hub *WebSocketHub, db *sql.DB, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка обновления соединения WebSocket:", err)
		return
	}
	hub.register <- conn

	// Отправка текущих новостей при подключении
	news, err := getAllNews(db)
	if err != nil {
		log.Println("Ошибка получения новостей:", err)
		return
	}
	hub.broadcast <- news

	// Обработка закрытия соединения
	go func() {
		defer func() {
			hub.unregister <- conn
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

func parseAndUpdate(db *sql.DB, hub *WebSocketHub) error {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://ldpr.ru/rss")
	if err != nil {
		return fmt.Errorf("ошибка парсинга RSS: %v", err)
	}

	for _, item := range feed.Items {
		title := unidecode.Unidecode(item.Title)
		link := unidecode.Unidecode(item.Link)
		description := unidecode.Unidecode(item.Description)
		var pubDate time.Time
		if item.PublishedParsed != nil {
			pubDate = *item.PublishedParsed
		} else {
			pubDate = time.Now()
		}

		// Вставка или обновление записи
		query := `
			INSERT INTO iu9Trofimenko (title, link, description, pub_date)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				title = VALUES(title),
				link = VALUES(link),
				description = VALUES(description),
				pub_date = VALUES(pub_date)
		`
		_, err = db.Exec(query, title, link, description, pubDate)
		if err != nil {
			log.Println("Ошибка вставки/обновления записи:", err)
			continue
		}
	}

	// После обновления базы данных, отправляем обновленные данные через WebSocket
	news, err := getAllNews(db)
	if err != nil {
		return fmt.Errorf("ошибка получения всех новостей: %v", err)
	}
	hub.broadcast <- news

	return nil
}

func getAllNews(db *sql.DB) ([]NewsItem, error) {
	rows, err := db.Query("SELECT id, title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var news []NewsItem
	for rows.Next() {
		var item NewsItem
		var title, link, description sql.NullString
		var pubDate sql.NullTime

		err := rows.Scan(&item.ID, &title, &link, &description, &pubDate)
		if err != nil {
			return nil, err
		}

		if title.Valid {
			item.Title = &title.String
		}
		if link.Valid {
			item.Link = &link.String
		}
		if description.Valid {
			item.Description = &description.String
		}
		if pubDate.Valid {
			formattedDate := pubDate.Time.Format("02.01.2006 15:04")
			item.PubDate = &formattedDate
		}

		news = append(news, item)
	}
	return news, nil
}
