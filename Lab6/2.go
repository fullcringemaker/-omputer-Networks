package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
)

type NewsItem struct {
	ID          int
	Title       string
	Description string
	Date        time.Time
	Link        string
}

var (
	db        *sql.DB
	clients   = make(map[*websocket.Conn]bool)
	broadcast = make(chan []NewsItem)
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	mutex = &sync.Mutex{}
)

func main() {
	// Set up database connection
	var err error
	// Include charset=utf8 in the DSN to handle Russian characters
	dsn := "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs?charset=utf8"
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Verify connection
	err = db.Ping()
	if err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// Initial update from RSS
	updateNewsFromRSS()

	// Start background goroutine to periodically update from RSS
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			updateNewsFromRSS()
		}
	}()

	// Start background goroutine to periodically check for updates
	go pollUpdates()

	// Start HTTP server
	http.HandleFunc("/", serveDashboard)
	http.HandleFunc("/ws", handleWebSocket)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	log.Println("Server started on :9742")
	err = http.ListenAndServe(":9742", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles("dashboard.html", "parser.html")
	if err != nil {
		http.Error(w, "Error parsing template", http.StatusInternalServerError)
		return
	}

	// Get the latest news items from the database
	newsItems, err := getAllNewsItems()
	if err != nil {
		http.Error(w, "Error fetching news items", http.StatusInternalServerError)
		return
	}

	data := struct {
		NewsItems []NewsItem
	}{
		NewsItems: newsItems,
	}

	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

func getAllNewsItems() ([]NewsItem, error) {
	rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko ORDER BY date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newsItems []NewsItem

	for rows.Next() {
		var item NewsItem
		var dateStr string
		err := rows.Scan(&item.ID, &item.Title, &item.Description, &dateStr, &item.Link)
		if err != nil {
			return nil, err
		}
		// Parse the date string
		item.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)
		newsItems = append(newsItems, item)
	}
	return newsItems, nil
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer ws.Close()

	// Register client
	mutex.Lock()
	clients[ws] = true
	mutex.Unlock()

	// Keep the connection open
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			mutex.Lock()
			delete(clients, ws)
			mutex.Unlock()
			ws.Close()
			break
		}
	}
}

func pollUpdates() {
	var previousNewsItems []NewsItem

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C

		newsItems, err := getAllNewsItems()
		if err != nil {
			log.Println("Error fetching news items:", err)
			continue
		}

		// Compare with previousNewsItems
		if !newsItemsEqual(newsItems, previousNewsItems) {
			// Update previousNewsItems
			previousNewsItems = newsItems

			// Send to clients
			mutex.Lock()
			for client := range clients {
				err := client.WriteJSON(newsItems)
				if err != nil {
					log.Println("Error sending to client:", err)
					client.Close()
					delete(clients, client)
				}
			}
			mutex.Unlock()
		}
	}
}

func newsItemsEqual(a, b []NewsItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func updateNewsFromRSS() {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://news.rambler.ru/rss/technology/")
	if err != nil {
		log.Println("Error parsing RSS feed:", err)
		return
	}

	for _, item := range feed.Items {
		// Check if the item exists in the database
		exists, err := newsItemExists(item.Link)
		if err != nil {
			log.Println("Error checking if news item exists:", err)
			continue
		}

		if !exists {
			// Insert the news item into the database
			err = insertNewsItem(item)
			if err != nil {
				log.Println("Error inserting news item:", err)
				continue
			}
		} else {
			// Check if the item in the database is different (e.g., corrupted)
			dbItem, err := getNewsItemByLink(item.Link)
			if err != nil {
				log.Println("Error getting news item by link:", err)
				continue
			}
			if dbItem.Title != item.Title || dbItem.Description != item.Description {
				// Update the item in the database
				err = updateNewsItem(dbItem.ID, item)
				if err != nil {
					log.Println("Error updating news item:", err)
					continue
				}
			}
		}
	}
}

func newsItemExists(link string) (bool, error) {
	var id int
	err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE link = ?", link).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func getNewsItemByLink(link string) (NewsItem, error) {
	var item NewsItem
	var dateStr string
	err := db.QueryRow("SELECT id, title, description, date, link FROM iu9Trofimenko WHERE link = ?", link).Scan(&item.ID, &item.Title, &item.Description, &dateStr, &item.Link)
	if err != nil {
		return item, err
	}
	item.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)
	return item, nil
}

func insertNewsItem(rssItem *gofeed.Item) error {
	var date time.Time
	if rssItem.PublishedParsed != nil {
		date = *rssItem.PublishedParsed
	} else {
		date = time.Now()
	}
	_, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)",
		rssItem.Title, rssItem.Description, date.Format("2006-01-02 15:04:05"), rssItem.Link)
	return err
}

func updateNewsItem(id int, rssItem *gofeed.Item) error {
	var date time.Time
	if rssItem.PublishedParsed != nil {
		date = *rssItem.PublishedParsed
	} else {
		date = time.Now()
	}
	_, err := db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, date = ?, link = ? WHERE id = ?",
		rssItem.Title, rssItem.Description, date.Format("2006-01-02 15:04:05"), rssItem.Link, id)
	return err
}
