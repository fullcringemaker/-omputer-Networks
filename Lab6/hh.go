package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
	_ "github.com/go-sql-driver/mysql"
	"github.com/rainycape/unidecode"
)

const (
	dbHost     = "students.yss.su"
	dbName     = "iu9networkslabs"
	dbUser     = "iu9networkslabs"
	dbPassword = "Je2dTYr6"
	rssURL     = "https://kinolexx.ru/rss"
	checkPeriod = 1 * time.Minute
	port       = ":9742"
)

var (
	tmplParser     *template.Template
	tmplDashboard  *template.Template
	connections    = make(map[*websocket.Conn]bool)
	mu             sync.Mutex
	upgrader       = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
)

type NewsItem struct {
	ID          int
	Title       string
	Link        string
	Description string
	PubDate     time.Time
}

// Подключение к базе данных
func connectDB() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", dbUser, dbPassword, dbHost, dbName)
	return sql.Open("mysql", dsn)
}

// Парсинг RSS
func fetchRSS() ([]NewsItem, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(rssURL)
	if err != nil {
		return nil, err
	}

	var news []NewsItem
	for _, item := range feed.Items {
		news = append(news, NewsItem{
			Title:       unidecode.Unidecode(item.Title),
			Link:        item.Link,
			Description: unidecode.Unidecode(item.Description),
			PubDate:     *item.PublishedParsed,
		})
	}
	return news, nil
}

// Обновление базы данных
func updateDB(db *sql.DB, news []NewsItem) error {
	for _, item := range news {
		_, err := db.Exec(`
            INSERT INTO iu9Trofimenko (title, link, description, pub_date)
            VALUES (?, ?, ?, ?)
            ON DUPLICATE KEY UPDATE
                title = VALUES(title),
                link = VALUES(link),
                description = VALUES(description),
                pub_date = VALUES(pub_date)
        `, item.Title, item.Link, item.Description, item.PubDate)
		if err != nil {
			return err
		}
	}
	return nil
}

// Отправка обновлений по WebSocket
func broadcastUpdate() {
	mu.Lock()
	defer mu.Unlock()

	for conn := range connections {
		err := conn.WriteMessage(websocket.TextMessage, []byte("update"))
		if err != nil {
			conn.Close()
			delete(connections, conn)
		}
	}
}

// Обновление новостей
func updateNews(db *sql.DB) {
	for {
		news, err := fetchRSS()
		if err != nil {
			log.Println("Error fetching RSS:", err)
			continue
		}
		err = updateDB(db, news)
		if err != nil {
			log.Println("Error updating DB:", err)
		}
		broadcastUpdate()
		time.Sleep(checkPeriod)
	}
}

// Обработчик WebSocket
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket error:", err)
		return
	}
	mu.Lock()
	connections[conn] = true
	mu.Unlock()
}

// Обработчик дэшборда
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	tmplDashboard.Execute(w, nil)
}

func main() {
	db, err := connectDB()
	if err != nil {
		log.Fatal("Database connection error:", err)
	}
	defer db.Close()

	// Запуск обновления новостей в фоне
	go updateNews(db)

	// Загрузка шаблонов
	tmplParser = template.Must(template.ParseFiles("parser.html"))
	tmplDashboard = template.Must(template.ParseFiles("dashboard.html"))

	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/dashboard", dashboardHandler)

	log.Println("Server started on port", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

