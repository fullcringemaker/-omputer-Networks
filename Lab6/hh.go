package main

import (
    "crypto/md5"
    "database/sql"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "html/template"
    "io/ioutil"
    "log"
    "net/http"
    "strings"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/websocket"
    "github.com/PuerkitoBio/goquery"
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

    // Reset AUTO_INCREMENT to start from 1
    _, err = db.Exec("ALTER TABLE iu9Trofimenko AUTO_INCREMENT = 1")
    if err != nil {
        log.Println("Error resetting AUTO_INCREMENT:", err)
    }

    // Check if the table exists, create it if it doesn't
    _, err = db.Exec(`
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT AUTO_INCREMENT PRIMARY KEY,
        title TEXT,
        description TEXT,
        date VARCHAR(10),
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
        // Parse Website
        feedItems, err := parseWebsite(db)
        if err != nil {
            log.Println("Error parsing website:", err)
            http.Error(w, "Error parsing website", http.StatusInternalServerError)
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
        {"title", "TEXT"},
        {"description", "TEXT"},
        {"date", "VARCHAR(10)"},
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

func parseWebsite(db *sql.DB) ([]map[string]string, error) {
    var feedItems []map[string]string

    // Fetch the page
    res, err := http.Get("https://news.rambler.ru/technology")
    if err != nil {
        return nil, err
    }
    defer res.Body.Close()

    if res.StatusCode != 200 {
        return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
    }

    // Load the HTML document
    doc, err := goquery.NewDocumentFromReader(res.Body)
    if err != nil {
        return nil, err
    }

    // Find the news items
    doc.Find(".topline__newsitem, .listing__newsitem").Each(func(i int, s *goquery.Selection) {
        title := s.Find(".news-item__title").Text()
        link, _ := s.Find("a").Attr("href")
        description := s.Find(".news-item__text").Text()
        date := s.Find(".news-item__date").Text()

        // Clean up the data
        title = strings.TrimSpace(title)
        description = strings.TrimSpace(description)
        date = strings.TrimSpace(date)

        // Convert date to dd.mm.yyyy format if possible
        if date != "" {
            parsedDate, err := time.Parse("02:15", date) // Adjust date format as needed
            if err == nil {
                date = parsedDate.Format("02.01.2006")
            } else {
                date = time.Now().Format("02.01.2006")
            }
        } else {
            date = time.Now().Format("02.01.2006")
        }

        // Add to feedItems
        item := map[string]string{
            "Title":       title,
            "Description": description,
            "Link":        link,
            "Date":        date,
        }
        feedItems = append(feedItems, item)

        // Check if the news item exists in the database
        var count int
        err := db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko WHERE title=? AND date=?", title, date).Scan(&count)
        if err != nil {
            log.Println("Error checking if news item exists:", err)
            return
        }

        if count == 0 {
            // Insert the news item into the database
            _, err = db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)",
                title, description, date, link)
            if err != nil {
                log.Println("Error inserting news item:", err)
                return
            }
            log.Println("Inserted new news item:", title)
            sendNewsUpdate(db)
        } else {
            // Check if the description has changed
            var dbDescription string
            err = db.QueryRow("SELECT description FROM iu9Trofimenko WHERE title=? AND date=?", title, date).Scan(&dbDescription)
            if err != nil {
                log.Println("Error retrieving existing news item:", err)
                return
            }
            if dbDescription != description {
                // Update the news item in the database
                _, err = db.Exec("UPDATE iu9Trofimenko SET description=?, link=? WHERE title=? AND date=?",
                    description, link, title, date)
                if err != nil {
                    log.Println("Error updating news item:", err)
                    return
                }
                log.Println("Updated news item:", title)
                sendNewsUpdate(db)
            }
        }
    })

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
    rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko ORDER BY id DESC")
    if err != nil {
        log.Println("Error querying news items:", err)
        return
    }
    defer rows.Close()

    var newsItems []map[string]string

    for rows.Next() {
        var id int
        var title, description, date, link string
        err = rows.Scan(&id, &title, &description, &date, &link)
        if err != nil {
            log.Println("Error scanning news item:", err)
            continue
        }
        item := map[string]string{
            "id":          fmt.Sprintf("%d", id),
            "title":       title,
            "description": description,
            "date":        date,
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
    rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko ORDER BY id DESC")
    if err != nil {
        log.Println("Error querying news items:", err)
        return
    }
    defer rows.Close()

    var newsItems []map[string]string

    for rows.Next() {
        var id int
        var title, description, date, link string
        err = rows.Scan(&id, &title, &description, &date, &link)
        if err != nil {
            log.Println("Error scanning news item:", err)
            continue
        }
        item := map[string]string{
            "id":          fmt.Sprintf("%d", id),
            "title":       title,
            "description": description,
            "date":        date,
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
    var prevHash string

    for {
        // Sleep for some time
        time.Sleep(5 * time.Second)

        // Get the data from the database
        rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko ORDER BY id DESC")
        if err != nil {
            log.Println("Error querying news items:", err)
            continue
        }

        var data []byte
        var newsItems []map[string]string

        for rows.Next() {
            var id int
            var title, description, date, link string
            err = rows.Scan(&id, &title, &description, &date, &link)
            if err != nil {
                log.Println("Error scanning news item:", err)
                continue
            }
            item := map[string]string{
                "id":          fmt.Sprintf("%d", id),
                "title":       title,
                "description": description,
                "date":        date,
                "link":        link,
            }
            newsItems = append(newsItems, item)
        }
        rows.Close()

        data, err = json.Marshal(newsItems)
        if err != nil {
            log.Println("Error marshalling news items:", err)
            continue
        }

        // Compute the hash of the data
        hash := md5.Sum(data)
        hashStr := hex.EncodeToString(hash[:])

        if hashStr != prevHash {
            sendNewsUpdate(db)
            prevHash = hashStr
        }
    }
}
