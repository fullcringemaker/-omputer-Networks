package main

import (
    "database/sql"
    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/websocket"
    "github.com/SlyMarbo/rss"
    "log"
    "net/http"
    "sync"
    "time"
    "fmt"
)

const (
    dbHost     = "students.yss.su"
    dbName     = "iu9networkslabs"
    dbUser     = "iu9networkslabs"
    dbPassword = "Je2dTYr6"
    tableName  = "iu9Trofimenko"
)

const rssFeedURL = "https://rospotrebnadzor.ru/region/rss/rss.php?rss=y"

var upgrader = websocket.Upgrader{}
var clients = make(map[*Client]bool)
var clientsMutex = sync.Mutex{}
var db *sql.DB

type Client struct {
    conn *websocket.Conn
}

type NewsItem struct {
    ID          int    `json:"id"`
    Title       string `json:"title"`
    Description string `json:"description"`
    Link        string `json:"link"`
    Date        string `json:"date"`
}

func main() {
    var err error
    // Connect to the database
    db, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?charset=utf8",
        dbUser, dbPassword, dbHost, dbName))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Ensure the table has the required structure
    err = ensureTable(db)
    if err != nil {
        log.Fatal(err)
    }

    // Start the RSS processing goroutine
    go func() {
        for {
            err := processRSSFeed(db)
            if err != nil {
                log.Println("Error processing RSS feed:", err)
            }
            time.Sleep(60 * time.Second)
        }
    }()

    // Start the database monitoring goroutine
    go func() {
        for {
            newsItems, err := getAllNewsItems(db)
            if err != nil {
                log.Println("Error getting news items:", err)
            } else {
                broadcastNewsItems(newsItems)
            }
            time.Sleep(5 * time.Second)
        }
    }()

    // Set up the HTTP handlers
    http.HandleFunc("/dashboard.html", dashboardHandler)
    http.HandleFunc("/ws", websocketHandler)

    // Serve static files
    http.Handle("/", http.FileServer(http.Dir(".")))

    // Start the server
    log.Println("Server started at :9742")
    err = http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("ListenAndServe: ", err)
    }
}

func ensureTable(db *sql.DB) error {
    // Check if the table exists
    var tableNameInDB string
    query := fmt.Sprintf("SHOW TABLES LIKE '%s'", tableName)
    err := db.QueryRow(query).Scan(&tableNameInDB)
    if err != nil {
        if err == sql.ErrNoRows || tableNameInDB == "" {
            // Table does not exist, create it
            return createTable(db)
        } else {
            return err
        }
    } else {
        // Table exists, ensure it has the correct columns
        return alterTable(db)
    }
}

func createTable(db *sql.DB) error {
    query := fmt.Sprintf(`CREATE TABLE %s (
        id INT AUTO_INCREMENT PRIMARY KEY,
        title VARCHAR(255),
        description TEXT,
        link VARCHAR(255),
        date VARCHAR(10),
        UNIQUE KEY unique_link (link)
    )`, tableName)
    _, err := db.Exec(query)
    return err
}

func alterTable(db *sql.DB) error {
    // Get existing columns
    columns := make(map[string]bool)
    rows, err := db.Query(fmt.Sprintf("SHOW COLUMNS FROM %s", tableName))
    if err != nil {
        return err
    }
    defer rows.Close()
    for rows.Next() {
        var field, dataType, null, key, defaultValue, extra string
        err = rows.Scan(&field, &dataType, &null, &key, &defaultValue, &extra)
        if err != nil {
            return err
        }
        columns[field] = true
    }

    // Add missing columns
    queries := []string{}
    if !columns["link"] {
        queries = append(queries, fmt.Sprintf("ALTER TABLE %s ADD COLUMN link VARCHAR(255)", tableName))
    }
    if !columns["date"] {
        queries = append(queries, fmt.Sprintf("ALTER TABLE %s ADD COLUMN date VARCHAR(10)", tableName))
    }
    if !columns["description"] {
        queries = append(queries, fmt.Sprintf("ALTER TABLE %s ADD COLUMN description TEXT", tableName))
    }
    if !columns["title"] {
        queries = append(queries, fmt.Sprintf("ALTER TABLE %s ADD COLUMN title VARCHAR(255)", tableName))
    }

    for _, query := range queries {
        _, err := db.Exec(query)
        if err != nil {
            return err
        }
    }

    // Add unique key on link
    if !columns["link"] {
        _, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD UNIQUE KEY unique_link (link)", tableName))
        if err != nil {
            return err
        }
    }

    return nil
}

func processRSSFeed(db *sql.DB) error {
    feed, err := rss.Fetch(rssFeedURL)
    if err != nil {
        return err
    }

    for _, item := range feed.Items {
        err := upsertNewsItem(db, item)
        if err != nil {
            log.Println("Error updating news item:", err)
        }
    }

    return nil
}

func upsertNewsItem(db *sql.DB, item *rss.Item) error {
    // Format the date as dd.mm.yyyy
    date := item.Date.Format("02.01.2006")

    // Use Content or Summary as description
    description := item.Content
    if description == "" {
        description = item.Summary
    }

    // Check if the item exists
    var id int
    query := fmt.Sprintf("SELECT id FROM %s WHERE link = ?", tableName)
    err := db.QueryRow(query, item.Link).Scan(&id)
    if err != nil {
        if err == sql.ErrNoRows {
            // Insert new item
            query = fmt.Sprintf("INSERT INTO %s (title, description, link, date) VALUES (?, ?, ?, ?)", tableName)
            _, err := db.Exec(query, item.Title, description, item.Link, date)
            return err
        } else {
            return err
        }
    } else {
        // Item exists, check if it needs to be updated
        query = fmt.Sprintf("UPDATE %s SET title = ?, description = ?, date = ? WHERE id = ?", tableName)
        _, err := db.Exec(query, item.Title, description, date, id)
        return err
    }
}

func getAllNewsItems(db *sql.DB) ([]NewsItem, error) {
    query := fmt.Sprintf("SELECT id, title, description, link, date FROM %s ORDER BY date DESC", tableName)
    rows, err := db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var newsItems []NewsItem
    for rows.Next() {
        var item NewsItem
        err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Link, &item.Date)
        if err != nil {
            return nil, err
        }
        newsItems = append(newsItems, item)
    }
    return newsItems, nil
}

func broadcastNewsItems(newsItems []NewsItem) {
    clientsMutex.Lock()
    defer clientsMutex.Unlock()

    for client := range clients {
        err := client.conn.WriteJSON(newsItems)
        if err != nil {
            log.Println("Error broadcasting to client:", err)
            client.conn.Close()
            delete(clients, client)
        }
    }
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "dashboard.html")
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Upgrade error:", err)
        return
    }

    client := &Client{conn: conn}

    clientsMutex.Lock()
    clients[client] = true
    clientsMutex.Unlock()

    // Send the current news items to the new client
    newsItems, err := getAllNewsItems(db)
    if err != nil {
        log.Println("Error getting news items:", err)
        return
    }

    err = conn.WriteJSON(newsItems)
    if err != nil {
        log.Println("Error sending news items to client:", err)
        return
    }

    // Handle client messages (if any)
    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            log.Println("Client disconnected:", err)
            clientsMutex.Lock()
            delete(clients, client)
            clientsMutex.Unlock()
            conn.Close()
            break
        }
    }
}
