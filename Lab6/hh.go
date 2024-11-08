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

// NewsItem представляет структуру новости
type NewsItem struct {
    Title       string
    Link        string
    Description string
    PubDate     time.Time
}

// WebSocketHub управляет подключениями WebSocket
type WebSocketHub struct {
    clients    map[*websocket.Conn]bool
    broadcast  chan []NewsItem
    register   chan *websocket.Conn
    unregister chan *websocket.Conn
    mu         sync.Mutex
}

func newHub() *WebSocketHub {
    return &WebSocketHub{
        clients:    make(map[*websocket.Conn]bool),
        broadcast:  make(chan []NewsItem),
        register:   make(chan *websocket.Conn),
        unregister: make(chan *websocket.Conn),
    }
}

func (h *WebSocketHub) run() {
    for {
        select {
        case conn := <-h.register:
            h.mu.Lock()
            h.clients[conn] = true
            h.mu.Unlock()
        case conn := <-h.unregister:
            h.mu.Lock()
            if _, ok := h.clients[conn]; ok {
                delete(h.clients, conn)
                conn.Close()
            }
            h.mu.Unlock()
        case news := <-h.broadcast:
            h.mu.Lock()
            for conn := range h.clients {
                err := conn.WriteJSON(news)
                if err != nil {
                    log.Printf("Ошибка отправки сообщения: %v", err)
                    conn.Close()
                    delete(h.clients, conn)
                }
            }
            h.mu.Unlock()
        }
    }
}

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

func serveWs(hub *WebSocketHub, db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            log.Printf("Ошибка апгрейда WebSocket: %v", err)
            return
        }
        hub.register <- conn

        // Отправить текущие новости при подключении
        news, err := fetchAllNews(db)
        if err != nil {
            log.Printf("Ошибка получения новостей: %v", err)
        } else {
            err = conn.WriteJSON(news)
            if err != nil {
                log.Printf("Ошибка отправки новостей: %v", err)
                conn.Close()
                hub.unregister <- conn
            }
        }

        // Чтение сообщений не требуется, поэтому просто слушаем закрытие соединения
        go func() {
            defer func() {
                hub.unregister <- conn
            }()
            for {
                _, _, err := conn.ReadMessage()
                if err != nil {
                    break
                }
            }
        }()
    }
}

func main() {
    // Подключение к базе данных
    dsn := "iu9networkslabs:Je2dTYr6@tcp(students.yss.su:3306)/iu9networkslabs"
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        log.Fatalf("Не удалось подключиться к базе данных: %v", err)
    }
    defer db.Close()

    // Проверка подключения
    if err := db.Ping(); err != nil {
        log.Fatalf("Не удалось проверить подключение к базе данных: %v", err)
    }

    hub := newHub()
    go hub.run()

    // Маршруты
    http.HandleFunc("/ws", serveWs(hub, db))
    http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "dashboard.html")
    })
    http.HandleFunc("/parser", func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "parser.html")
    })

    // Запуск парсинга и обновления каждые 5 минут
    go func() {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        for {
            parseAndUpdate(db, hub)
            <-ticker.C
        }
    }()

    // Первоначальный запуск
    parseAndUpdate(db, hub)

    // Проверка на пустую таблицу и восстановление через 1 минуту
    go func() {
        for {
            time.Sleep(30 * time.Second)
            count, err := countNews(db)
            if err != nil {
                log.Printf("Ошибка подсчета новостей: %v", err)
                continue
            }
            if count == 0 {
                log.Println("Таблица пуста. Восстановление новостей через 1 минуту...")
                time.AfterFunc(1*time.Minute, func() {
                    parseAndUpdate(db, hub)
                })
            }
        }
    }()

    // Запуск веб-сервера
    fmt.Println("Сервер запущен на порту 9742")
    if err := http.ListenAndServe(":9742", nil); err != nil {
        log.Fatalf("Ошибка запуска сервера: %v", err)
    }
}

// parseAndUpdate выполняет парсинг RSS и обновляет базу данных
func parseAndUpdate(db *sql.DB, hub *WebSocketHub) {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL("https://kinolexx.ru/rss")
    if err != nil {
        log.Printf("Ошибка парсинга RSS: %v", err)
        return
    }

    for _, item := range feed.Items {
        title := unidecode.Unidecode(item.Title)
        link := unidecode.Unidecode(item.Link)
        description := unidecode.Unidecode(item.Description)
        var pubDate time.Time
        if item.PublishedParsed != nil {
            pubDate = *item.PublishedParsed
        } else {
            pubDate = time.Now()
        }

        // Вставка с обновлением при дубликате
        _, err := db.Exec(`INSERT INTO iu9Trofimenko (title, link, description, pub_date)
            VALUES (?, ?, ?, ?)
            ON DUPLICATE KEY UPDATE
                title = VALUES(title),
                link = VALUES(link),
                description = VALUES(description),
                pub_date = VALUES(pub_date)`,
            title, link, description, pubDate)
        if err != nil {
            log.Printf("Ошибка вставки новости: %v", err)
        }
    }

    // Получение обновленного списка новостей
    news, err := fetchAllNews(db)
    if err != nil {
        log.Printf("Ошибка получения новостей: %v", err)
        return
    }

    // Отправка обновлений через WebSocket
    hub.broadcast <- news
}

// fetchAllNews извлекает все новости из базы данных
func fetchAllNews(db *sql.DB) ([]NewsItem, error) {
    rows, err := db.Query(`SELECT title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var news []NewsItem
    for rows.Next() {
        var item NewsItem
        var pubDate string
        if err := rows.Scan(&item.Title, &item.Link, &item.Description, &pubDate); err != nil {
            return nil, err
        }
        item.PubDate, err = time.Parse("2006-01-02 15:04:05", pubDate)
        if err != nil {
            item.PubDate = time.Now()
        }
        news = append(news, item)
    }
    return news, nil
}

// countNews возвращает количество новостей в таблице
func countNews(db *sql.DB) (int, error) {
    var count int
    err := db.QueryRow(`SELECT COUNT(*) FROM iu9Trofimenko`).Scan(&count)
    return count, err
}
