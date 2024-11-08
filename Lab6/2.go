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
	"github.com/rainycape/unidecode"
	_ "github.com/go-sql-driver/mysql"
)

const (
	rssURL   = "https://ldpr.ru/rss"
	dbDriver = "mysql"
	dbHost   = "students.yss.su"
	dbPort   = "3306"
	dbUser   = "iu9networkslabs"
	dbPass   = "Je2dTYr6"
	dbName   = "iu9networkslabs"
	dbTable  = "iu9Trofimenko"
)

var (
	clients   = make(map[*websocket.Conn]bool)
	broadcast = make(chan []NewsItem)
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	mu sync.Mutex
)

type NewsItem struct {
	ID      int
	Title   string
	Content string
	Author  string
	Date    string
	Link    string
}

func main() {
	db, err := connectDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	go rssUpdater(db)
	go dbWatcher(db)

	http.HandleFunc("/dashboard.html", dashboardHandler(db))
	http.HandleFunc("/ws", wsHandler)
	http.Handle("/parser.html", http.FileServer(http.Dir("./")))
	log.Println("Server started at :9742")
	log.Fatal(http.ListenAndServe(":9742", nil))
}

func connectDB() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true",
		dbUser, dbPass, dbHost, dbPort, dbName)
	return sql.Open(dbDriver, dsn)
}

func rssUpdater(db *sql.DB) {
	for {
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(rssURL)
		if err != nil {
			log.Println("Error parsing RSS:", err)
		} else {
			err = updateDatabase(db, feed.Items)
			if err != nil {
				log.Println("Error updating database:", err)
			}
		}
		time.Sleep(60 * time.Second)
	}
}

func updateDatabase(db *sql.DB, items []*gofeed.Item) error {
	for _, item := range items {
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM "+dbTable+" WHERE link=?)", item.Link).Scan(&exists)
		if err != nil {
			return err
		}

		title := unidecode.Unidecode(item.Title)
		content := unidecode.Unidecode(item.Content)
		if content == "" && item.Extensions != nil {
			if encodedContent, ok := item.Extensions["content"]["encoded"]; ok {
				if len(encodedContent) > 0 {
					content = unidecode.Unidecode(encodedContent[0].Value)
				}
			}
		}
		author := ""
		if item.Author != nil {
			author = unidecode.Unidecode(item.Author.Name)
		}
		date := ""
		if item.PublishedParsed != nil {
			date = item.PublishedParsed.Format("02.01.2006")
		} else {
			date = time.Now().Format("02.01.2006")
		}

		if !exists {
			_, err = db.Exec("INSERT INTO "+dbTable+" (title, content, author, date, link) VALUES (?, ?, ?, ?, ?)",
				title, content, author, date, item.Link)
			if err != nil {
				return err
			}
		} else {
			var existingTitle, existingContent, existingAuthor, existingDate string
			err = db.QueryRow("SELECT title, content, author, date FROM "+dbTable+" WHERE link=?", item.Link).
				Scan(&existingTitle, &existingContent, &existingAuthor, &existingDate)
			if err != nil {
				return err
			}
			if existingTitle != title || existingContent != content || existingAuthor != author || existingDate != date {
				_, err = db.Exec("UPDATE "+dbTable+" SET title=?, content=?, author=?, date=? WHERE link=?",
					title, content, author, date, item.Link)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func dashboardHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("parser.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		newsItems, err := getNewsItems(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, newsItems)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func getNewsItems(db *sql.DB) ([]NewsItem, error) {
	rows, err := db.Query("SELECT id, title, content, author, date, link FROM " + dbTable + " ORDER BY date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newsItems []NewsItem
	for rows.Next() {
		var item NewsItem
		err := rows.Scan(&item.ID, &item.Title, &item.Content, &item.Author, &item.Date, &item.Link)
		if err != nil {
			return nil, err
		}
		newsItems = append(newsItems, item)
	}
	return newsItems, nil
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer ws.Close()

	mu.Lock()
	clients[ws] = true
	mu.Unlock()

	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			mu.Lock()
			delete(clients, ws)
			mu.Unlock()
			break
		}
	}
}

func dbWatcher(db *sql.DB) {
	var lastNewsItems []NewsItem
	for {
		newsItems, err := getNewsItems(db)
		if err != nil {
			log.Println("Error getting news items:", err)
			time.Sleep(1 * time.Second)
			continue
		}
		if !newsItemsEqual(newsItems, lastNewsItems) {
			lastNewsItems = newsItems
			mu.Lock()
			for client := range clients {
				err := client.WriteJSON(newsItems)
				if err != nil {
					log.Println("WebSocket write error:", err)
					client.Close()
					delete(clients, client)
				}
			}
			mu.Unlock()
		}
		time.Sleep(1 * time.Second)
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
