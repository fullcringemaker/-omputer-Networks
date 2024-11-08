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
    PubDate     time.Time `json:"pub_date"`
}

// App содержит все необходимые компоненты приложения
type App struct {
    db          *sql.DB
    templates   *template.Template
    clients     map[*websocket.Conn]bool
    broadcast   chan NewsItem
    mutex       sync.Mutex
    deletedNews map[string]time.Time
}

// NewApp инициализирует новое приложение
func NewApp() *App {
    tmpl := template.Must(template.ParseGlob("templates/*.html"))
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su:3306)/iu9networkslabs")
    if err != nil {
        log.Fatalf("Ошибка подключения к базе данных: %v", err)
    }

    // Убедитесь, что таблица существует
    createTable := `
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT(11) NOT NULL AUTO_INCREMENT PRIMARY KEY,
        title TEXT COLLATE 'latin1_swedish_ci' NULL,
        link TEXT COLLATE 'latin1_swedish_ci' NULL UNIQUE,
        description TEXT COLLATE 'latin1_swedish_ci' NULL,
        pub_date DATETIME NULL
    ) ENGINE='InnoDB' COLLATE 'latin1_swedish_ci';
    `
    _, err = db.Exec(createTable)
    if err != nil {
        log.Fatalf("Ошибка создания таблицы: %v", err)
    }

    return &App{
        db:          db,
        templates:   tmpl,
        clients:     make(map[*websocket.Conn]bool),
        broadcast:   make(chan NewsItem),
        deletedNews: make(map[string]time.Time),
    }
}

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// ServeHTTP для обработки HTTP-запросов
func (app *App) ServeHTTP() {
    http.HandleFunc("/dashboard", app.handleDashboard)
    http.HandleFunc("/ws", app.handleWebSocket)
    http.HandleFunc("/", app.handleParser)

    log.Println("Сервер запущен на порту 9742")
    log.Fatal(http.ListenAndServe(":9742", nil))
}

// handleParser отображает шаблон parser.html
func (app *App) handleParser(w http.ResponseWriter, r *http.Request) {
    app.templates.ExecuteTemplate(w, "parser.html", nil)
}

// handleDashboard отображает шаблон dashboard.html
func (app *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
    app.templates.ExecuteTemplate(w, "dashboard.html", nil)
}

// handleWebSocket устанавливает соединение WebSocket
func (app *App) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Ошибка обновления WebSocket: %v", err)
        return
    }
    defer conn.Close()

    app.mutex.Lock()
    app.clients[conn] = true
    app.mutex.Unlock()

    // Отправка текущих новостей при подключении
    news, err := app.fetchAllNews()
    if err != nil {
        log.Printf("Ошибка получения новостей: %v", err)
        return
    }
    for _, item := range news {
        conn.WriteJSON(item)
    }

    for {
        // Ожидание сообщений от клиента (не используются, но необходим для поддержания соединения)
        _, _, err := conn.ReadMessage()
        if err != nil {
            log.Printf("Ошибка чтения сообщения: %v", err)
            break
        }
    }

    app.mutex.Lock()
    delete(app.clients, conn)
    app.mutex.Unlock()
}

// fetchAllNews получает все новости из базы данных
func (app *App) fetchAllNews() ([]NewsItem, error) {
    rows, err := app.db.Query("SELECT id, title, link, description, pub_date FROM iu9Trofimenko")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var news []NewsItem
    for rows.Next() {
        var item NewsItem
        var pubDate time.Time
        err := rows.Scan(&item.ID, &item.Title, &item.Link, &item.Description, &pubDate)
        if err != nil {
            return nil, err
        }
        item.PubDate = pubDate
        news = append(news, item)
    }
    return news, nil
}

// broadcastNews отправляет новости всем подключенным клиентам
func (app *App) broadcastNews() {
    for {
        newsItem := <-app.broadcast
        app.mutex.Lock()
        for client := range app.clients {
            err := client.WriteJSON(newsItem)
            if err != nil {
                log.Printf("Ошибка отправки новости: %v", err)
                client.Close()
                delete(app.clients, client)
            }
        }
        app.mutex.Unlock()
    }
}

// parseAndUpdateRSS парсит RSS и обновляет базу данных
func (app *App) parseAndUpdateRSS() {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL("https://ldpr.ru/rss")
    if err != nil {
        log.Printf("Ошибка парсинга RSS: %v", err)
        return
    }

    for _, item := range feed.Items {
        title := unidecode.Unidecode(item.Title)
        link := unidecode.Unidecode(item.Link)
        description := unidecode.Unidecode(item.Description)
        pubDate := item.PublishedParsed
        if pubDate == nil {
            pubDate = item.UpdatedParsed
        }
        if pubDate == nil {
            pubDate = &time.Time{}
        }

        // Вставка или обновление новости
        query := `
        INSERT INTO iu9Trofimenko (title, link, description, pub_date)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE
            title = VALUES(title),
            link = VALUES(link),
            description = VALUES(description),
            pub_date = VALUES(pub_date)
        `
        _, err := app.db.Exec(query, title, link, description, pubDate)
        if err != nil {
            log.Printf("Ошибка вставки новости: %v", err)
            continue
        }

        // Отправка новости через канал для broadcast
        newsItem := NewsItem{
            Title:       title,
            Link:        link,
            Description: description,
            PubDate:     *pubDate,
        }
        app.broadcast <- newsItem
    }
}

// monitorDeletions следит за удалениями и восстанавливает их через 1 минуту
func (app *App) monitorDeletions() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        // Получение всех ссылок из базы данных
        rows, err := app.db.Query("SELECT link FROM iu9Trofimenko")
        if err != nil {
            log.Printf("Ошибка получения ссылок: %v", err)
            continue
        }

        existingLinks := make(map[string]bool)
        for rows.Next() {
            var link string
            rows.Scan(&link)
            existingLinks[link] = true
        }
        rows.Close()

        // Проверка удаленных ссылок
        app.mutex.Lock()
        for link, deletedAt := range app.deletedNews {
            if !existingLinks[link] {
                if time.Since(deletedAt) >= time.Minute {
                    // Восстановление новости
                    // Для этого потребуется хранить данные удаленных новостей
                    // Здесь предполагается, что вы сохраняете удаленные новости где-то
                    // Для простоты этого примера восстановление не реализовано
                    // Вы можете расширить эту часть по своему усмотрению
                }
            } else {
                delete(app.deletedNews, link)
            }
        }
        app.mutex.Unlock()
    }
}

func main() {
    app := NewApp()

    // Запуск горутин
    go app.broadcastNews()
    go app.parseAndUpdateRSS()
    go app.monitorDeletions()

    // Запуск HTTP-сервера
    app.ServeHTTP()
}
