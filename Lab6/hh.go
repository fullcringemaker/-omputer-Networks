package main

import (
    "compress/gzip"
    "database/sql"
    "encoding/xml"
    "fmt"
    "io"
    "log"
    "net/http"
    "strings"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/websocket"
    "golang.org/x/net/html/charset"
)

const (
    rssURL        = "https://rospotrebnadzor.ru/region/rss/rss.php?rss=y"
    websocketPort = ":9742"
)

// RSS structures
type RSS struct {
    Channel Channel `xml:"channel"`
}

type Channel struct {
    Items []Item `xml:"item"`
}

type Item struct {
    Title       string `xml:"title"`
    Description string `xml:"description"`
    PubDate     string `xml:"pubDate"`
}

// Database configuration
const (
    dbUser     = "iu9networkslabs"
    dbPassword = "Je2dTYr6"
    dbName     = "iu9networkslabs"
    dbHost     = "students.yss.su"
)

// WebSocket upgrader
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// Global WebSocket connections
var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan []Item)

func main() {
    // Start the WebSocket server
    http.HandleFunc("/ws", handleConnections)
    http.Handle("/", http.FileServer(http.Dir("./")))

    go handleMessages()

    fmt.Println("Server started on port 9742")
    go func() {
        log.Fatal(http.ListenAndServe(websocketPort, nil))
    }()

    // Start the RSS parsing and database updating
    for {
        items, err := fetchRSSItems()
        if err != nil {
            log.Println("Error fetching RSS items:", err)
            time.Sleep(10 * time.Minute)
            continue
        }

        err = updateDatabase(items)
        if err != nil {
            log.Println("Error updating database:", err)
        }

        // Send updated items to WebSocket clients
        broadcast <- items

        // Sleep before the next update
        time.Sleep(10 * time.Minute)
    }
}

// Fetch RSS items from the RSS feed
func fetchRSSItems() ([]Item, error) {
    client := &http.Client{}
    req, err := http.NewRequest("GET", rssURL, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Accept-Encoding", "gzip")

    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var reader io.Reader

    switch resp.Header.Get("Content-Encoding") {
    case "gzip":
        gzReader, err := gzip.NewReader(resp.Body)
        if err != nil {
            return nil, err
        }
        defer gzReader.Close()
        reader = gzReader
    default:
        reader = resp.Body
    }

    decoder := xml.NewDecoder(reader)
    decoder.CharsetReader = charset.NewReaderLabel

    var rss RSS
    err = decoder.Decode(&rss)
    if err != nil {
        return nil, err
    }

    return rss.Channel.Items, nil
}

// Update the database with new RSS items
func updateDatabase(items []Item) error {
    // Connect to the database
    dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8", dbUser, dbPassword, dbHost, dbName)
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        return err
    }
    defer db.Close()

    // Create table if not exists
    createTableQuery := `
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT AUTO_INCREMENT PRIMARY KEY,
        title VARCHAR(255) NOT NULL,
        description TEXT NOT NULL,
        date VARCHAR(20) NOT NULL
    ) CHARACTER SET utf8 COLLATE utf8_general_ci;`
    _, err = db.Exec(createTableQuery)
    if err != nil {
        return err
    }

    // Fetch existing titles to avoid duplicates
    existingTitles := make(map[string]bool)
    rows, err := db.Query("SELECT title FROM iu9Trofimenko")
    if err != nil {
        return err
    }
    defer rows.Close()
    for rows.Next() {
        var title string
        rows.Scan(&title)
        existingTitles[title] = true
    }

    // Insert or update items
    for _, item := range items {
        if existingTitles[item.Title] {
            // Check if the content has changed
            var dbDescription string
            err = db.QueryRow("SELECT description FROM iu9Trofimenko WHERE title = ?", item.Title).Scan(&dbDescription)
            if err != nil {
                return err
            }
            if dbDescription != item.Description {
                // Update the existing record
                _, err = db.Exec("UPDATE iu9Trofimenko SET description = ?, date = ? WHERE title = ?", item.Description, formatDate(item.PubDate), item.Title)
                if err != nil {
                    return err
                }
            }
            continue
        }
        // Insert new item
        insertQuery := `INSERT INTO iu9Trofimenko (title, description, date) VALUES (?, ?, ?)`
        _, err = db.Exec(insertQuery, item.Title, item.Description, formatDate(item.PubDate))
        if err != nil {
            return err
        }
    }

    return nil
}

// Format date to dd.mm.yyyy
func formatDate(dateStr string) string {
    // Try different date formats
    layouts := []string{
        time.RFC1123Z,
        time.RFC1123,
        "Mon, 02 Jan 2006 15:04:05 -0700",
        "Mon, 02 Jan 2006 15:04:05 MST",
        "02 Jan 2006 15:04:05 -0700",
        "02 Jan 2006",
    }

    var t time.Time
    var err error
    for _, layout := range layouts {
        t, err = time.Parse(layout, dateStr)
        if err == nil {
            return t.Format("02.01.2006")
        }
    }
    // If parsing fails, return the original string
    return dateStr
}

// Handle WebSocket connections
func handleConnections(w http.ResponseWriter, r *http.Request) {
    // Upgrade initial GET request to a WebSocket
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("WebSocket upgrade error:", err)
        return
    }
    // Register client
    clients[ws] = true

    // Send current data upon connection
    items, err := fetchDatabaseItems()
    if err == nil {
        ws.WriteJSON(items)
    }

    defer ws.Close()
    delete(clients, ws)
}

// Handle incoming messages
func handleMessages() {
    for {
        items := <-broadcast
        for client := range clients {
            err := client.WriteJSON(items)
            if err != nil {
                log.Println("WebSocket error:", err)
                client.Close()
                delete(clients, client)
            }
        }
    }
}

// Fetch items from the database
func fetchDatabaseItems() ([]Item, error) {
    // Connect to the database
    dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8", dbUser, dbPassword, dbHost, dbName)
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        return nil, err
    }
    defer db.Close()

    rows, err := db.Query("SELECT title, description, date FROM iu9Trofimenko ORDER BY id DESC")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var items []Item
    for rows.Next() {
        var item Item
        err := rows.Scan(&item.Title, &item.Description, &item.PubDate)
        if err != nil {
            return nil, err
        }
        items = append(items, item)
    }

    return items, nil
}
