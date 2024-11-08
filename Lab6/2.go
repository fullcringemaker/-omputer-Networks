package main

import (
    "database/sql"
    "encoding/xml"
    "fmt"
    "html/template"
    "io/ioutil"
    "log"
    "net/http"
    "sync"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/websocket"
    "github.com/mmcdole/gofeed"
    "github.com/rainycape/unidecode"
)

var (
    clients   = make(map[*websocket.Conn]bool)
    broadcast = make(chan []NewsItem)
    upgrader  = websocket.Upgrader{}
    mutex     = &sync.Mutex{}
)

// NewsItem represents a news item from the RSS feed
type NewsItem struct {
    ID          int
    Title       string
    Description string
    Date        time.Time
    Link        string
}

// Global variable to store the latest news items
var latestNews []NewsItem

func main() {
    // Start the WebSocket server
    http.HandleFunc("/ws", handleConnections)

    // Serve the dashboard
    http.HandleFunc("/", serveDashboard)

    // Start the background processes
    go handleMessages()
    go periodicUpdate()

    // Start the server on port 9742
    log.Println("Server started on :9742")
    err := http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("ListenAndServe: ", err)
    }
}

// periodicUpdate periodically updates the news and checks for database changes
func periodicUpdate() {
    for {
        updateNews()
        time.Sleep(10 * time.Second) // Adjust the interval as needed
    }
}

// updateNews parses the RSS feed and updates the database
func updateNews() {
    // Parse the RSS feed
    feedURL := "https://ldpr.ru/rss"
    feed, err := parseRSS(feedURL)
    if err != nil {
        log.Println("Error parsing RSS feed:", err)
        return
    }

    // Connect to the database
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs?charset=utf8")
    if err != nil {
        log.Println("Error connecting to the database:", err)
        return
    }
    defer db.Close()

    // Ensure the connection is available
    if err = db.Ping(); err != nil {
        log.Println("Database ping error:", err)
        return
    }

    // Fetch existing news from the database
    existingNews, err := fetchExistingNews(db)
    if err != nil {
        log.Println("Error fetching existing news:", err)
        return
    }

    // Map for quick lookup
    existingNewsMap := make(map[string]NewsItem)
    for _, news := range existingNews {
        existingNewsMap[news.Link] = news
    }

    // Insert or update news items
    for _, item := range feed.Items {
        // Decode Russian characters
        title := unidecode.Unidecode(item.Title)
        description := unidecode.Unidecode(item.Description)
        link := item.Link
        pubDate := item.PublishedParsed

        // Skip if pubDate is nil
        if pubDate == nil {
            continue
        }

        // Check if the item exists in the database
        existingItem, exists := existingNewsMap[link]

        if !exists {
            // Insert new item
            _, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)",
                title, description, pubDate.Format("2006-01-02 15:04:05"), link)
            if err != nil {
                log.Println("Error inserting news item:", err)
                continue
            }
        } else {
            // If the item exists but has been modified, re-insert it
            if existingItem.Title != title || existingItem.Description != description {
                _, err := db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, date = ? WHERE id = ?",
                    title, description, pubDate.Format("2006-01-02 15:04:05"), existingItem.ID)
                if err != nil {
                    log.Println("Error updating news item:", err)
                    continue
                }
            }
        }
    }

    // Fetch the updated news
    updatedNews, err := fetchExistingNews(db)
    if err != nil {
        log.Println("Error fetching updated news:", err)
        return
    }

    // Check for deletions (items in existingNews but not in feed)
    for _, existingItem := range existingNews {
        found := false
        for _, item := range feed.Items {
            if existingItem.Link == item.Link {
                found = true
                break
            }
        }
        if !found {
            // Re-insert the deleted item
            _, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)",
                existingItem.Title, existingItem.Description, existingItem.Date.Format("2006-01-02 15:04:05"), existingItem.Link)
            if err != nil {
                log.Println("Error re-inserting deleted news item:", err)
                continue
            }
        }
    }

    // Update the global news list
    mutex.Lock()
    latestNews = updatedNews
    mutex.Unlock()

    // Send the updated news to all clients
    broadcast <- updatedNews
}

// parseRSS parses the RSS feed from the given URL
func parseRSS(url string) (*gofeed.Feed, error) {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL(url)
    if err != nil {
        return nil, err
    }
    return feed, nil
}

// fetchExistingNews fetches news items from the database
func fetchExistingNews(db *sql.DB) ([]NewsItem, error) {
    rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko ORDER BY date DESC")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var newsItems []NewsItem
    for rows.Next() {
        var news NewsItem
        var dateStr string
        err := rows.Scan(&news.ID, &news.Title, &news.Description, &dateStr, &news.Link)
        if err != nil {
            return nil, err
        }
        news.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)
        newsItems = append(newsItems, news)
    }
    return newsItems, nil
}

// serveDashboard serves the dashboard.html file
func serveDashboard(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "dashboard.html")
}

// handleConnections upgrades HTTP connections to WebSocket connections
func handleConnections(w http.ResponseWriter, r *http.Request) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("WebSocket upgrade error:", err)
        return
    }
    defer ws.Close()

    // Register the new client
    clients[ws] = true

    // Send the latest news to the new client
    mutex.Lock()
    initialNews := latestNews
    mutex.Unlock()
    ws.WriteJSON(initialNews)

    for {
        // Keep the connection open
        _, _, err := ws.ReadMessage()
        if err != nil {
            log.Println("WebSocket read error:", err)
            delete(clients, ws)
            break
        }
    }
}

// handleMessages sends news updates to all connected clients
func handleMessages() {
    for {
        news := <-broadcast
        for client := range clients {
            err := client.WriteJSON(news)
            if err != nil {
                log.Println("WebSocket write error:", err)
                client.Close()
                delete(clients, client)
            }
        }
    }
}
