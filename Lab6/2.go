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

type NewsItem struct {
    ID          int
    Guid        string
    Title       string
    Description string
    Date        string // in format dd.mm.yyyy
    Link        string
    Author      string
    Content     string
}

var (
    db           *sql.DB
    dbMutex      sync.Mutex
    clients      = make(map[*websocket.Conn]bool)
    clientsMutex sync.Mutex
    upgrader     = websocket.Upgrader{}
    templates    *template.Template
)

func main() {
    var err error
    // Database connection parameters
    dbUser := "iu9networkslabs"
    dbPassword := "Je2dTYr6"
    dbHost := "students.yss.su"
    dbName := "iu9networkslabs"

    dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4",
        dbUser, dbPassword, dbHost, dbName)

    db, err = sql.Open("mysql", dsn)
    if err != nil {
        log.Fatal("Error connecting to database:", err)
    }
    defer db.Close()

    // Parse templates
    templates = template.Must(template.ParseFiles("parser.html"))

    // Start the HTTP server
    http.HandleFunc("/", indexHandler)
    http.HandleFunc("/ws", wsHandler)
    http.HandleFunc("/dashboard.html", dashboardHandler)
    http.HandleFunc("/parser.html", parserHandler)

    // Start a goroutine to periodically parse RSS and update database
    go func() {
        for {
            updateNews()
            time.Sleep(10 * time.Second) // adjust as needed
        }
    }()

    // Start a goroutine to check for database changes and notify clients
    go func() {
        var lastNews []NewsItem
        for {
            news, err := getAllNews()
            if err != nil {
                log.Println("Error getting news from database:", err)
                time.Sleep(5 * time.Second)
                continue
            }
            if !newsEqual(news, lastNews) {
                lastNews = news
                broadcastNews(news)
            }
            time.Sleep(5 * time.Second)
        }
    }()

    log.Println("Server started on :9742")
    log.Fatal(http.ListenAndServe(":9742", nil))
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
    http.Redirect(w, r, "/dashboard.html", http.StatusFound)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "dashboard.html")
}

func parserHandler(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "parser.html")
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true } // allow all connections
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Upgrade error:", err)
        return
    }
    defer conn.Close()

    clientsMutex.Lock()
    clients[conn] = true
    clientsMutex.Unlock()

    // Send the current news to the client
    news, err := getAllNews()
    if err == nil {
        sendNewsToClient(conn, news)
    }

    // Keep the connection open
    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            clientsMutex.Lock()
            delete(clients, conn)
            clientsMutex.Unlock()
            conn.Close()
            break
        }
    }
}

func getAllNews() ([]NewsItem, error) {
    dbMutex.Lock()
    defer dbMutex.Unlock()
    rows, err := db.Query("SELECT id, guid, title, description, DATE_FORMAT(date, '%d.%m.%Y'), link, author, content FROM iu9Trofimenko ORDER BY id DESC")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var news []NewsItem
    for rows.Next() {
        var item NewsItem
        err := rows.Scan(&item.ID, &item.Guid, &item.Title, &item.Description, &item.Date, &item.Link, &item.Author, &item.Content)
        if err != nil {
            return nil, err
        }
        news = append(news, item)
    }
    return news, nil
}

func updateNews() {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL("https://ldpr.ru/rss")
    if err != nil {
        log.Println("Error parsing RSS feed:", err)
        return
    }

    for _, item := range feed.Items {
        guid := item.GUID
        title := unidecode.Unidecode(item.Title)
        description := unidecode.Unidecode(item.Description)
        link := item.Link
        author := unidecode.Unidecode(item.Author.Name)
        content := unidecode.Unidecode(item.Content)
        pubDate := item.PublishedParsed
        if pubDate == nil {
            pubDate = item.UpdatedParsed
        }
        var date string
        if pubDate != nil {
            date = pubDate.Format("2006-01-02")
        } else {
            date = time.Now().Format("2006-01-02")
        }

        // Check if the news item already exists
        var id int
        dbMutex.Lock()
        err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE guid = ?", guid).Scan(&id)
        dbMutex.Unlock()
        if err == sql.ErrNoRows {
            // Insert new news item
            dbMutex.Lock()
            _, err := db.Exec("INSERT INTO iu9Trofimenko (guid, title, description, date, link, author, content) VALUES (?, ?, ?, ?, ?, ?, ?)",
                guid, title, description, date, link, author, content)
            dbMutex.Unlock()
            if err != nil {
                log.Println("Error inserting news item:", err)
            } else {
                log.Println("Inserted new news item:", title)
            }
        } else if err != nil {
            log.Println("Error checking news item:", err)
        } else {
            // News item exists, check if it needs to be updated
            var existingTitle, existingDescription, existingContent string
            dbMutex.Lock()
            err := db.QueryRow("SELECT title, description, content FROM iu9Trofimenko WHERE guid = ?", guid).Scan(&existingTitle, &existingDescription, &existingContent)
            dbMutex.Unlock()
            if err != nil {
                log.Println("Error checking existing news item:", err)
                continue
            }
            if existingTitle != title || existingDescription != description || existingContent != content {
                // Update the news item
                dbMutex.Lock()
                _, err := db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, content = ?, date = ? WHERE guid = ?",
                    title, description, content, date, guid)
                dbMutex.Unlock()
                if err != nil {
                    log.Println("Error updating news item:", err)
                } else {
                    log.Println("Updated news item:", title)
                }
            }
        }
    }
}

func newsEqual(a, b []NewsItem) bool {
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

func broadcastNews(news []NewsItem) {
    clientsMutex.Lock()
    defer clientsMutex.Unlock()
    for client := range clients {
        err := sendNewsToClient(client, news)
        if err != nil {
            log.Println("Error writing to client:", err)
            client.Close()
            delete(clients, client)
        }
    }
}

func sendNewsToClient(conn *websocket.Conn, news []NewsItem) error {
    var renderedNews []string
    for _, item := range news {
        var buf string
        err := templates.ExecuteTemplate(&buf, "newsItem", item)
        if err != nil {
            return err
        }
        renderedNews = append(renderedNews, buf)
    }
    return conn.WriteJSON(renderedNews)
}
