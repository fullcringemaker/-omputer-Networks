package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "html/template"
    "log"
    "net/http"
    "sync"
    "time"

    "github.com/gorilla/websocket"
    _ "github.com/go-sql-driver/mysql"
    "github.com/mmcdole/gofeed"
    "github.com/rainycape/unidecode"
)

// NewsItem представляет структуру новости
type NewsItem struct {
    ID          int       `json:"id"`
    Title       string    `json:"title"`
    Link        string    `json:"link"`
    Description string    `json:"description"`
    PubDate     string    `json:"pub_date"`
}

// WebSocketHub управляет подключениями WebSocket
type WebSocketHub struct {
    clients    map[*websocket.Conn]bool
    broadcast  chan NewsItem
    register   chan *websocket.Conn
    unregister chan *websocket.Conn
    mu         sync.Mutex
}

func newHub() *WebSocketHub {
    return &WebSocketHub{
        clients:    make(map[*websocket.Conn]bool),
        broadcast:  make(chan NewsItem),
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
            message, err := json.Marshal(news)
            if err != nil {
                log.Println("Ошибка маршалинга:", err)
                continue
            }
            h.mu.Lock()
            for conn := range h.clients {
                err := conn.WriteMessage(websocket.TextMessage, message)
                if err != nil {
                    log.Println("Ошибка отправки сообщения:", err)
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

func serveWs(hub *WebSocketHub, w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Ошибка апгрейда:", err)
        return
    }
    hub.register <- conn

    // Удаление соединения при закрытии
    defer func() {
        hub.unregister <- conn
    }()
}

func main() {
    // Параметры подключения к базе данных
    dbUser := "iu9networkslabs"
    dbPassword := "Je2dTYr6"
    dbName := "iu9networkslabs"
    dbHost := "students.yss.su"

    // Подключение к базе данных
    dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8", dbUser, dbPassword, dbHost, dbName)
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        log.Fatalf("Не удалось подключиться к базе данных: %v", err)
    }
    defer db.Close()

    // Проверка подключения
    if err := db.Ping(); err != nil {
        log.Fatalf("Не удалось проверить подключение к базе данных: %v", err)
    }

    // Создание таблицы, если она не существует
    createTableQuery := `
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT(11) NOT NULL AUTO_INCREMENT PRIMARY KEY,
        title TEXT COLLATE 'latin1_swedish_ci' NULL,
        link TEXT COLLATE 'latin1_swedish_ci' NULL,
        description TEXT COLLATE 'latin1_swedish_ci' NULL,
        pub_date DATETIME NULL
    ) ENGINE='InnoDB' COLLATE='latin1_swedish_ci';
    `
    _, err = db.Exec(createTableQuery)
    if err != nil {
        log.Fatalf("Не удалось создать таблицу: %v", err)
    }

    // Инициализация WebSocket хаба
    hub := newHub()
    go hub.run()

    // Маршруты
    http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        serveWs(hub, w, r)
    })

    // Статические файлы
    http.Handle("/", http.FileServer(http.Dir("./")))

    // Шаблон parser.html
    tmpl, err := template.ParseFiles("parser.html")
    if err != nil {
        log.Fatalf("Не удалось загрузить шаблон: %v", err)
    }

    // Парсинг RSS и обновление базы данных
    go func() {
        fp := gofeed.NewParser()
        for {
            feed, err := fp.ParseURL("https://ldpr.ru/rss")
            if err != nil {
                log.Println("Ошибка парсинга RSS:", err)
                time.Sleep(10 * time.Minute)
                continue
            }

            for _, item := range feed.Items {
                // Обработка русских символов
                title := unidecode.Unidecode(item.Title)
                link := unidecode.Unidecode(item.Link)
                description := unidecode.Unidecode(item.Description)
                pubDate, err := time.Parse(time.RFC1123Z, item.Published)
                if err != nil {
                    pubDate = time.Now()
                }

                // Проверка наличия новости в базе
                var exists bool
                checkQuery := "SELECT EXISTS(SELECT 1 FROM iu9Trofimenko WHERE link = ?)"
                err = db.QueryRow(checkQuery, link).Scan(&exists)
                if err != nil {
                    log.Println("Ошибка проверки существования новости:", err)
                    continue
                }

                if !exists {
                    // Вставка новой новости
                    insertQuery := "INSERT INTO iu9Trofimenko (title, link, description, pub_date) VALUES (?, ?, ?, ?)"
                    _, err := db.Exec(insertQuery, title, link, description, pubDate)
                    if err != nil {
                        log.Println("Ошибка вставки новости:", err)
                        continue
                    }

                    // Получение последней вставленной новости
                    var news NewsItem
                    lastID := 0
                    err = db.QueryRow("SELECT LAST_INSERT_ID()").Scan(&lastID)
                    if err != nil {
                        log.Println("Ошибка получения последнего ID:", err)
                        continue
                    }
                    news.ID = lastID
                    news.Title = title
                    news.Link = link
                    news.Description = description
                    news.PubDate = pubDate.Format("02.01.2006")

                    // Отправка новости через WebSocket
                    hub.broadcast <- news
                }
            }

            // Повторный запуск через 10 минут
            time.Sleep(10 * time.Minute)
        }
    }()

    // Обработчик dashboard.html
    http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
        tmpl.Execute(w, nil)
    })

    // Запуск сервера на порту 9742
    log.Println("Сервер запущен на порту 9742")
    err = http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatalf("Ошибка запуска сервера: %v", err)
    }
}
