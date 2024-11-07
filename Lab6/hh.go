package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/gorilla/websocket"
	_ "github.com/go-sql-driver/mysql"
)

type NewsItem struct {
	ID          int
	Title       string
	Description string
	Date        string
}

var (
	db            *sql.DB
	clients       = make(map[*websocket.Conn]bool)
	broadcast     = make(chan []NewsItem)
	upgrader      = websocket.Upgrader{}
	clientsMutex  sync.Mutex
	lastNewsItems []NewsItem
)

func main() {
	var err error
	// Подключение к базе данных
	db, err = sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Создание таблицы, если она не существует
	createTable()

	// Запуск парсинга RSS-ленты в отдельной горутине
	go fetchRSSFeed()

	// Запуск мониторинга изменений в базе данных
	go monitorDatabaseChanges()

	// Запуск обработки сообщений для WebSocket клиентов
	go handleMessages()

	// Обслуживание статических файлов
	http.Handle("/", http.FileServer(http.Dir("./")))

	// WebSocket endpoint
	http.HandleFunc("/ws", handleConnections)

	// Endpoint для ручного запуска парсинга
	http.HandleFunc("/parse", parseHandler)

	// Запуск сервера на порту 9742
	fmt.Println("Server starting on port 9742")
	err = http.ListenAndServe(":9742", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func createTable() {
	query := `
	CREATE TABLE IF NOT EXISTS iu9Trofimenko (
		id INT AUTO_INCREMENT PRIMARY KEY,
		title VARCHAR(255),
		description TEXT,
		date VARCHAR(10)
	)
	`
	_, err := db.Exec(query)
	if err != nil {
		log.Fatal(err)
	}
}

func fetchRSSFeed() {
	for {
		fetchAndUpdateRSSFeed()
		time.Sleep(10 * time.Minute)
	}
}

func fetchAndUpdateRSSFeed() {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://rospotrebnadzor.ru/region/rss/rss.php?rss=y")
	if err != nil {
		log.Println("Error fetching RSS feed:", err)
		return
	}

	for _, item := range feed.Items {
		date := item.PublishedParsed.Format("02.01.2006")
		// Проверка существования новости в базе данных
		var id int
		err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE title=? AND date=?", item.Title, date).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				// Вставка новой новости
				_, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date) VALUES (?, ?, ?)",
					item.Title, item.Content, date)
				if err != nil {
					log.Println("Error inserting item:", err)
				} else {
					log.Println("Inserted new item:", item.Title)
				}
			} else {
				log.Println("Error querying item:", err)
			}
		}
	}

	// Отправка обновленных новостей клиентам
	newsItems := getNewsItems()
	broadcast <- newsItems
}

func getNewsItems() []NewsItem {
	rows, err := db.Query("SELECT id, title, description, date FROM iu9Trofimenko ORDER BY id DESC")
	if err != nil {
		log.Println("Error querying news items:", err)
		return nil
	}
	defer rows.Close()

	var newsItems []NewsItem
	for rows.Next() {
		var item NewsItem
		err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Date)
		if err != nil {
			log.Println("Error scanning news item:", err)
			continue
		}
		newsItems = append(newsItems, item)
	}

	return newsItems
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Обновление запроса до WebSocket
	upgrader.CheckOrigin = func(r *http.Request) bool { return true } // Разрешить любые источники
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	// Регистрация нового клиента
	clientsMutex.Lock()
	clients[ws] = true
	clientsMutex.Unlock()

	// Отправка текущих новостей новому клиенту
	newsItems := getNewsItems()
	err = ws.WriteJSON(newsItems)
	if err != nil {
		log.Println("Error sending initial news items:", err)
		ws.Close()
		clientsMutex.Lock()
		delete(clients, ws)
		clientsMutex.Unlock()
		return
	}

	for {
		var msg interface{}
		err := ws.ReadJSON(&msg)
		if err != nil {
			// Клиент отключился
			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()
			ws.Close()
			break
		}
	}
}

func handleMessages() {
	for {
		// Получение следующего сообщения из канала broadcast
		newsItems := <-broadcast

		// Отправка сообщения всем подключенным клиентам
		clientsMutex.Lock()
		for client := range clients {
			err := client.WriteJSON(newsItems)
			if err != nil {
				log.Printf("WebSocket error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
		clientsMutex.Unlock()
	}
}

func monitorDatabaseChanges() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		newsItems := getNewsItems()
		if !isEqual(newsItems, lastNewsItems) {
			lastNewsItems = newsItems
			broadcast <- newsItems
		}
	}
}

func isEqual(a, b []NewsItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i, itemA := range a {
		itemB := b[i]
		if itemA.ID != itemB.ID || itemA.Title != itemB.Title || itemA.Description != itemB.Description || itemA.Date != itemB.Date {
			return false
		}
	}
	return true
}

func parseHandler(w http.ResponseWriter, r *http.Request) {
	fetchAndUpdateRSSFeed()
	fmt.Fprintf(w, "Parsing completed")
}
