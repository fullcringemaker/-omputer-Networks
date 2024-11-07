package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "html/template"
    "log"
    "net/http"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/websocket"
    "github.com/mmcdole/gofeed"
)

var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan []byte)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

func main() {
    // Connect to the database
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Check if the table exists, create it if it doesn't
    _, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT AUTO_INCREMENT PRIMARY KEY,
        guid VARCHAR(255) UNIQUE,
        title TEXT,
        description TEXT,
        pubDate VARCHAR(10),
        link TEXT
    )
    `)
    if err != nil {
        log.Fatal(err)
    }

    // Ensure the table has all required columns
    err = ensureTableSchema(db)
    if err != nil {
        log.Fatal(err)
    }

    // Start Goroutines
    go handleMessages()
    go monitorDatabaseChanges(db)

    // Handle parser.html
    http.HandleFunc("/parser.html", func(w http.ResponseWriter, r *http.Request) {
        // Parse RSS feed
        feedItems, err := parseRSS(db)
        if err != nil {
            log.Println("Error parsing RSS feed:", err)
            http.Error(w, "Error parsing RSS feed", http.StatusInternalServerError)
            return
        }

        // Render parser.html template with feed items
        tmpl, err := template.ParseFiles("parser.html")
        if err != nil {
            log.Println("Error parsing template:", err)
            http.Error(w, "Error parsing template", http.StatusInternalServerError)
            return
        }

        err = tmpl.Execute(w, feedItems)
        if err != nil {
            log.Println("Error executing template:", err)
            http.Error(w, "Error executing template", http.StatusInternalServerError)
            return
        }
    })

    // Handle dashboard.html
    http.HandleFunc("/dashboard.html", func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "dashboard.html")
    })

    // Websocket handler
    http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        serveWs(db, w, r)
    })

    log.Println("Server started on port 9742")
    err = http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("ListenAndServe: ", err)
    }
}

// ensureTableSchema ensures that the 'iu9Trofimenko' table has all required columns
func ensureTableSchema(db *sql.DB) error {
    requiredColumns := []struct {
        Name string
        Type string
    }{
        {"id", "INT AUTO_INCREMENT PRIMARY KEY"},
        {"guid", "VARCHAR(255) UNIQUE"},
        {"title", "TEXT"},
        {"description", "TEXT"},
        {"pubDate", "VARCHAR(10)"},
        {"link", "TEXT"},
    }

    for _, col := range requiredColumns {
        var count int
        err := db.QueryRow("SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'iu9Trofimenko' AND COLUMN_NAME = ?", col.Name).Scan(&count)
        if err != nil {
            return err
        }
        if count == 0 {
            // Add missing column
            _, err := db.Exec(fmt.Sprintf("ALTER TABLE iu9Trofimenko ADD COLUMN %s %s", col.Name, col.Type))
            if err != nil {
                return err
            }
            log.Printf("Added '%s' column to 'iu9Trofimenko' table", col.Name)
        }
    }
    return nil
}

func parseRSS(db *sql.DB) ([]map[string]string, error) {
    feedURL := "https://news.rambler.ru/rss/technology/"
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL(feedURL)
    if err != nil {
        return nil, err
    }

    var feedItems []map[string]string

    for _, item := range feed.Items {
        guid := item.GUID
        if guid == "" {
            guid = item.Link // Fallback to link if GUID is empty
        }

        title := item.Title
        description := item.Description
        link := item.Link

        // Format pubDate to dd.mm.yyyy
        var dateStr string
        if item.PublishedParsed != nil {
            dateStr = item.PublishedParsed.Format("02.01.2006")
        } else {
            dateStr = ""
        }

        // Check if the news item exists in the database
        var count int
        err = db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko WHERE guid=?", guid).Scan(&count)
        if err != nil {
            log.Println("Error checking if news item exists:", err)
            continue
        }

        if count == 0 {
            // Insert the news item into the database
            _, err = db.Exec("INSERT INTO iu9Trofimenko (guid, title, description, pubDate, link) VALUES (?, ?, ?, ?, ?)",
                guid, title, description, dateStr, link)
            if err != nil {
                log.Println("Error inserting news item:", err)
                continue
            }
            log.Println("Inserted new news item:", title)
            sendNewsUpdate(db)
        } else {
            // Check if the title or description differs
            var dbTitle, dbDescription string
            err = db.QueryRow("SELECT title, description FROM iu9Trofimenko WHERE guid=?", guid).Scan(&dbTitle, &dbDescription)
            if err != nil {
                log.Println("Error retrieving existing news item:", err)
                continue
            }
            if dbTitle != title || dbDescription != description {
                // Insert the news item into the database
                _, err = db.Exec("INSERT INTO iu9Trofimenko (guid, title, description, pubDate, link) VALUES (?, ?, ?, ?, ?)",
                    guid, title, description, dateStr, link)
                if err != nil {
                    log.Println("Error inserting updated news item:", err)
                    continue
                }
                log.Println("Inserted updated news item:", title)
                sendNewsUpdate(db)
            }
        }

        // Add item to feedItems to pass to the template
        feedItems = append(feedItems, map[string]string{
            "Title":       title,
            "Description": description,
            "Link":        link,
            "PubDate":     dateStr,
        })
    }

    return feedItems, nil
}

func serveWs(db *sql.DB, w http.ResponseWriter, r *http.Request) {
    // Upgrade initial GET request to a websocket
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Upgrade error:", err)
        return
    }
    // Make sure we close the connection when the function returns
    defer ws.Close()

    // Register the new client
    clients[ws] = true

    // Send the latest news items to the new client
    sendNewsUpdateToClient(db, ws)

    // Start listening for messages
    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            log.Println("Read error:", err)
            delete(clients, ws)
            break
        }
        // Handle received message (if any)
    }
}

func sendNewsUpdate(db *sql.DB) {
    // Get the list of news items from the database
    rows, err := db.Query("SELECT id, title, description, pubDate, link FROM iu9Trofimenko ORDER BY id DESC")
    if err != nil {
        log.Println("Error querying news items:", err)
        return
    }
    defer rows.Close()

    var newsItems []map[string]string

    for rows.Next() {
        var id int
        var title, description, pubDate, link string
        err = rows.Scan(&id, &title, &description, &pubDate, &link)
        if err != nil {
            log.Println("Error scanning news item:", err)
            continue
        }
        item := map[string]string{
            "id":          fmt.Sprintf("%d", id),
            "title":       title,
            "description": description,
            "pubDate":     pubDate,
            "link":        link,
        }
        newsItems = append(newsItems, item)
    }

    // Convert newsItems to JSON
    data, err := json.Marshal(newsItems)
    if err != nil {
        log.Println("Error marshalling news items:", err)
        return
    }

    // Send the JSON data to all connected clients
    broadcast <- data
}

func sendNewsUpdateToClient(db *sql.DB, ws *websocket.Conn) {
    // Get the list of news items from the database
    rows, err := db.Query("SELECT id, title, description, pubDate, link FROM iu9Trofimenko ORDER BY id DESC")
    if err != nil {
        log.Println("Error querying news items:", err)
        return
    }
    defer rows.Close()

    var newsItems []map[string]string

    for rows.Next() {
        var id int
        var title, description, pubDate, link string
        err = rows.Scan(&id, &title, &description, &pubDate, &link)
        if err != nil {
            log.Println("Error scanning news item:", err)
            continue
        }
        item := map[string]string{
            "id":          fmt.Sprintf("%d", id),
            "title":       title,
            "description": description,
            "pubDate":     pubDate,
            "link":        link,
        }
        newsItems = append(newsItems, item)
    }

    // Convert newsItems to JSON
    data, err := json.Marshal(newsItems)
    if err != nil {
        log.Println("Error marshalling news items:", err)
        return
    }

    // Send the JSON data to the client
    err = ws.WriteMessage(websocket.TextMessage, data)
    if err != nil {
        log.Printf("Websocket error: %v", err)
        ws.Close()
        delete(clients, ws)
    }
}

func handleMessages() {
    for {
        // Grab the next message from the broadcast channel
        msg := <-broadcast
        // Send it out to every client that is currently connected
        for client := range clients {
            err := client.WriteMessage(websocket.TextMessage, msg)
            if err != nil {
                log.Printf("Websocket error: %v", err)
                client.Close()
                delete(clients, client)
            }
        }
    }
}

func monitorDatabaseChanges(db *sql.DB) {
    var prevCount int

    for {
        // Sleep for some time
        time.Sleep(5 * time.Second)

        // Get the count of news items
        var count int
        err := db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko").Scan(&count)
        if err != nil {
            log.Println("Error counting news items:", err)
            continue
        }

        // Compare with previous count
        if count != prevCount {
            // News items have changed
            // Send update to clients
            sendNewsUpdate(db)
            prevCount = count
        }
    }
}
