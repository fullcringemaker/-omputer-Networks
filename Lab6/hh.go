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
    PubDate     time.Time `json:"pub_date"`
}

// App содержит все необходимые компоненты приложения
type App struct {
    db          *sql.DB
    templates   *template.Template
    clients     map[*websocket.Conn]bool
    broadcast   chan NewsItem
    mutex       sync.Mutex
    deletedNews map[string]DeletedNews
}

// DeletedNews хранит информацию об удаленных новостях
type DeletedNews struct {
    Item      NewsItem
    DeletedAt time.Time
}

// NewApp инициализирует новое приложение
func NewApp() *App {
    tmpl := template.Must(template.ParseFiles("dashboard.html", "parser.html"))
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su:3306)/iu9networkslabs?charset=latin1&parseTime=true")
    if err != nil {
        log.Fatalf("Ошибка подключения к базе данных: %v", err)
    }

    // Убедитесь, что таблица существует
    createTable := `
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT(11) NOT NULL AUTO_INCREMENT PRIMARY KEY,
        title VARCHAR(1024) COLLATE 'latin1_swedish_ci' NULL,
        link VARCHAR(2083) COLLATE 'latin1_swedish_ci' NOT NULL UNIQUE,
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
        deletedNews: make(map[string]DeletedNews),
    }
}

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

// ServeHTTP настраивает маршруты и запускает сервер
func (app *App) ServeHTTP() {
    http.HandleFunc("/dashboard", app.handleDashboard)
    http.HandleFunc("/ws", app.handleWebSocket)
    http.HandleFunc("/", app.handleParser)

    log.Println("Сервер запущен на порту 9742")
    log.Fatal(http.ListenAndServe(":9742", nil))
}

// handleParser отображает шаблон parser.html с текущими новостями
func (app *App) handleParser(w http.ResponseWriter, r *http.Request) {
    news, err := app.fetchAllNews()
    if err != nil {
        http.Error(w, "Ошибка получения новостей", http.StatusInternalServerError)
        return
    }
    app.templates.ExecuteTemplate(w, "parser.html", news)
}

// handleDashboard отображает шаблон dashboard.html
func (app *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
    app.templates.ExecuteTemplate(w, "dashboard.html", nil)
}

// handleWebSocket устанавливает соединение WebSocket и отправляет текущие новости
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
        // Ожидание сообщений от клиента (не используются, но необходимо для поддержания соединения)
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
    rows, err := app.db.Query("SELECT id, title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC")
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

        // Получение ID вставленной или обновленной записи
        var id int
        err = app.db.QueryRow("SELECT id FROM iu9Trofimenko WHERE link = ?", link).Scan(&id)
        if err != nil {
            log.Printf("Ошибка получения ID новости: %v", err)
            continue
        }

        // Отправка новости через канал для broadcast
        newsItem := NewsItem{
            ID:          id,
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
        rows, err := app.db.Query("SELECT link, title, description, pub_date FROM iu9Trofimenko")
        if err != nil {
            log.Printf("Ошибка получения ссылок: %v", err)
            continue
        }

        existingLinks := make(map[string]bool)
        for rows.Next() {
            var link string
            var title string
            var description string
            var pubDate time.Time
            err := rows.Scan(&link, &title, &description, &pubDate)
            if err != nil {
                log.Printf("Ошибка сканирования строки: %v", err)
                continue
            }
            existingLinks[link] = true
        }
        rows.Close()

        app.mutex.Lock()
        for link, deletedNews := range app.deletedNews {
            if !existingLinks[link] {
                if time.Since(deletedNews.DeletedAt) >= time.Minute {
                    // Восстановление новости
                    query := `
                    INSERT INTO iu9Trofimenko (title, link, description, pub_date)
                    VALUES (?, ?, ?, ?)
                    `
                    _, err := app.db.Exec(query, deletedNews.Item.Title, deletedNews.Item.Link, deletedNews.Item.Description, deletedNews.Item.PubDate)
                    if err != nil {
                        log.Printf("Ошибка восстановления новости: %v", err)
                        continue
                    }
                    // Отправка восстановленной новости через канал для broadcast
                    app.broadcast <- deletedNews.Item
                    delete(app.deletedNews, link)
                }
            } else {
                delete(app.deletedNews, link)
            }
        }
        app.mutex.Unlock()
    }
}

// watchDeletions следит за удалениями в таблице и добавляет их в deletedNews
func (app *App) watchDeletions() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        // Получение всех ссылок из RSS
        fp := gofeed.NewParser()
        feed, err := fp.ParseURL("https://ldpr.ru/rss")
        if err != nil {
            log.Printf("Ошибка парсинга RSS для watchDeletions: %v", err)
            continue
        }

        rssLinks := make(map[string]bool)
        for _, item := range feed.Items {
            link := unidecode.Unidecode(item.Link)
            rssLinks[link] = true
        }

        // Получение всех ссылок из базы данных
        rows, err := app.db.Query("SELECT link, title, description, pub_date FROM iu9Trofimenko")
        if err != nil {
            log.Printf("Ошибка получения ссылок из базы данных для watchDeletions: %v", err)
            continue
        }

        dbLinks := make(map[string]NewsItem)
        for rows.Next() {
            var link, title, description string
            var pubDate time.Time
            err := rows.Scan(&link, &title, &description, &pubDate)
            if err != nil {
                log.Printf("Ошибка сканирования строки базы данных для watchDeletions: %v", err)
                continue
            }
            dbLinks[link] = NewsItem{
                Title:       title,
                Link:        link,
                Description: description,
                PubDate:     pubDate,
            }
        }
        rows.Close()

        app.mutex.Lock()
        for link, newsItem := range dbLinks {
            if !rssLinks[link] {
                // Новость удалена из RSS или вручную удалена из базы
                if _, exists := app.deletedNews[link]; !exists {
                    app.deletedNews[link] = DeletedNews{
                        Item:      newsItem,
                        DeletedAt: time.Now(),
                    }
                }
            }
        }
        app.mutex.Unlock()
    }
}

func main() {
    app := NewApp()

    // Запуск горутин
    go app.broadcastNews()

    // Парсинг и обновление RSS каждые 5 минут
    go func() {
        for {
            app.parseAndUpdateRSS()
            time.Sleep(5 * time.Minute)
        }
    }()

    // Мониторинг удалений
    go app.monitorDeletions()

    // Наблюдение за удалениями
    go app.watchDeletions()

    // Запуск HTTP-сервера
    app.ServeHTTP()
}
