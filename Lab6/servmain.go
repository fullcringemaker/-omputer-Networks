package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
)

type NewsItem struct {
	Title       string    `json:"Title"`
	Link        string    `json:"Link"`
	Description string    `json:"Description"`
	PubDate     time.Time `json:"PubDate"`
}

type WebSocketHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []NewsItem
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex
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
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()
		case news := <-h.broadcast:
			h.mu.Lock()
			for conn := range h.clients {
				err := conn.WriteJSON(news)
				if err != nil {
					log.Printf("Ошибка отправки сообщения: %v", err)
					conn.Close()
					delete(h.clients, conn)
				}
			}
			h.mu.Unlock()
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func fetchAllNews(db *sql.DB) ([]NewsItem, error) {
	rows, err := db.Query(`SELECT title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var news []NewsItem
	for rows.Next() {
		var item NewsItem
		var pubDate time.Time
		if err := rows.Scan(&item.Title, &item.Link, &item.Description, &pubDate); err != nil {
			return nil, err
		}
		item.PubDate = pubDate
		news = append(news, item)
	}
	return news, nil
}

func serveWs(hub *WebSocketHub, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Ошибка апгрейда WebSocket: %v", err)
			return
		}
		hub.register <- conn

		// Отправить текущие новости при подключении
		news, err := fetchAllNews(db)
		if err != nil {
			log.Printf("Ошибка получения новостей: %v", err)
		} else {
			err = conn.WriteJSON(news)
			if err != nil {
				log.Printf("Ошибка отправки новостей: %v", err)
				conn.Close()
				hub.unregister <- conn
			}
		}

		// Чтение сообщений не требуется, поэтому просто слушаем закрытие соединения
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
}

func parseAndUpdate(db *sql.DB, hub *WebSocketHub) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://ldpr.ru/RSS")
	if err != nil {
		log.Printf("Ошибка парсинга RSS: %v", err)
		return
	}

	// Используем подготовленные выражения для повышения производительности
	stmt, err := db.Prepare(`INSERT INTO iu9Trofimenko (title, link, description, pub_date)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE
            title = VALUES(title),
            description = VALUES(description),
            pub_date = VALUES(pub_date)`)
	if err != nil {
		log.Printf("Ошибка подготовки запроса: %v", err)
		return
	}
	defer stmt.Close()

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

		// Вставка с обновлением при дубликате
		_, err := stmt.Exec(title, link, description, pubDate)
		if err != nil {
			log.Printf("Ошибка вставки/обновления новости: %v", err)
		}
	}

	// Получение обновленного списка новостей
	news, err := fetchAllNews(db)
	if err != nil {
		log.Printf("Ошибка получения новостей: %v", err)
		return
	}

	// Отправка обновлений через WebSocket
	hub.broadcast <- news
}

func countNews(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM iu9Trofimenko`).Scan(&count)
	return count, err
}

func monitorDatabase(db *sql.DB, hub *WebSocketHub, interval time.Duration, timerDuration time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastNewsHash string
	var timer *time.Timer
	var timerActive bool
	var timerMu sync.Mutex

	for range ticker.C {
		news, err := fetchAllNews(db)
		if err != nil {
			log.Printf("Ошибка при мониторинге базы данных: %v", err)
			continue
		}

		// Создание хеша для текущего списка новостей
		newsJSON, _ := json.Marshal(news)
		currentHash := fmt.Sprintf("%x", md5.Sum(newsJSON))

		if currentHash != lastNewsHash {
			lastNewsHash = currentHash
			hub.broadcast <- news
		}

		// Проверка, пуста ли таблица
		if len(news) == 0 {
			timerMu.Lock()
			if !timerActive {
				log.Println("Таблица пуста. Запуск таймера на 1 минуту для восстановления данных...")
				timer = time.AfterFunc(timerDuration, func() {
					log.Println("Таймер завершён. Восстановление данных из RSS...")
					parseAndUpdate(db, hub)

					// После восстановления проверяем, заполнилась ли таблица
					newCount, err := countNews(db)
					if err != nil {
						log.Printf("Ошибка подсчёта новостей после восстановления: %v", err)
						return
					}
					if newCount > 0 {
						timerMu.Lock()
						timerActive = false
						timerMu.Unlock()
						log.Println("Данные успешно восстановлены. Таймер отключён.")
					}
				})
				timerActive = true
			}
			timerMu.Unlock()
		} else {
			// Если таблица не пуста, отменяем таймер, если он активен
			timerMu.Lock()
			if timerActive && timer != nil {
				log.Println("Таблица заполнена. Отмена активного таймера.")
				timer.Stop()
				timerActive = false
			}
			timerMu.Unlock()
		}
	}
}

func main() {
	// Подключение к базе данных
	dsn := "iu9networkslabs:Je2dTYr6@tcp(students.yss.su:3306)/iu9networkslabs?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Не удалось подключиться к базе данных: %v", err)
	}
	defer db.Close()

	// Проверка подключения
	if err := db.Ping(); err != nil {
		log.Fatalf("Не удалось проверить подключение к базе данных: %v", err)
	}

	hub := newHub()
	go hub.run()

	// Маршруты
	http.HandleFunc("/ws", serveWs(hub, db))
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "dashboard.html")
	})
	http.HandleFunc("/parser", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "parser.html")
	})

	// Запуск веб-сервера
	server := &http.Server{
		Addr: "185.102.139.168:9742", // Изменено на конкретный IP и порт
	}

	go func() {
		log.Println("Запуск веб-сервера на адресе http://185.102.139.168:9742")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Ошибка запуска сервера: %v", err)
		}
	}()

	// Первоначальный запуск парсинга в отдельной горутине для минимизации задержки
	go parseAndUpdate(db, hub)

	// Запуск мониторинга базы данных каждые 2 секунды с таймером восстановления в 1 минуту
	go monitorDatabase(db, hub, 2*time.Second, 1*time.Minute)

	// Запуск парсинга и обновления каждые 5 минут
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			parseAndUpdate(db, hub)
			<-ticker.C
		}
	}()

	// Ожидание завершения (необходимо для того, чтобы main не завершился)
	select {}
}
