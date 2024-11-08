package main

import (
    "database/sql"
    "html/template"
    "log"
    "net/http"
    "strings"
    "sync"
    "time"

    "github.com/gorilla/mux"
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
    PubDate     string
}

var (
    db          *sql.DB
    tmplDash    *template.Template
    tmplParser  *template.Template
    clients     = make(map[*websocket.Conn]bool)
    broadcast   = make(chan string)
    mu          sync.Mutex
)

// Константы конфигурации
const (
    rssURL       = "https://ldpr.ru/rss"
    dbUser       = "iu9networkslabs"
    dbPassword   = "Je2dTYr6"
    dbName       = "iu9networkslabs"
    dbHost       = "students.yss.su"
    tableName    = "iu9Trofimenko"
    port         = ":9742"
    reconnectSec = 60 // 1 минута в секундах
)

func main() {
    var err error
    // Подключение к базе данных
    dsn := dbUser + ":" + dbPassword + "@tcp(" + dbHost + ")/" + dbName + "?parseTime=true"
    db, err = sql.Open("mysql", dsn)
    if err != nil {
        log.Fatalf("Ошибка подключения к базе данных: %v", err)
    }
    defer db.Close()

    if err = db.Ping(); err != nil {
        log.Fatalf("Не удалось подключиться к базе данных: %v", err)
    }

    log.Println("Успешно подключено к базе данных.")

    // Загрузка шаблонов
    tmplDash, err = template.ParseFiles("dashboard.html")
    if err != nil {
        log.Fatalf("Ошибка загрузки шаблона dashboard.html: %v", err)
    }

    tmplParser, err = template.ParseFiles("parser.html")
    if err != nil {
        log.Fatalf("Ошибка загрузки шаблона parser.html: %v", err)
    }

    // Настройка роутера
    router := mux.NewRouter()
    router.HandleFunc("/", dashboardHandler)
    router.HandleFunc("/ws", handleConnections)

    // Запуск вебсокет обработчика
    go handleMessages()

    // Запуск парсинга RSS и обновления БД
    go startParser()

    // Запуск HTTP сервера
    log.Printf("Сервер запущен на http://localhost%s", port)
    if err := http.ListenAndServe(port, router); err != nil {
        log.Fatalf("Ошибка запуска сервера: %v", err)
    }
}

// dashboardHandler обрабатывает запросы к главной странице
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
    news, err := fetchNewsFromDB()
    if err != nil {
        http.Error(w, "Ошибка получения новостей из базы данных.", http.StatusInternalServerError)
        return
    }
    err = tmplDash.Execute(w, news)
    if err != nil {
        log.Printf("Ошибка рендеринга шаблона dashboard: %v", err)
    }
}

// upgrader используется для обновления HTTP-соединения до WebSocket
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true // В продакшене рекомендуется более строгая проверка
    },
}

// handleConnections обрабатывает WebSocket соединения
func handleConnections(w http.ResponseWriter, r *http.Request) {
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Ошибка подключения WebSocket: %v", err)
        return
    }
    defer ws.Close()

    // Добавляем клиента
    mu.Lock()
    clients[ws] = true
    mu.Unlock()

    log.Println("Новый клиент подключился.")

    // Чтение сообщений от клиента (необходимо для обнаружения закрытия)
    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            log.Printf("Ошибка чтения сообщения от клиента: %v", err)
            mu.Lock()
            delete(clients, ws)
            mu.Unlock()
            break
        }
    }
}

// handleMessages отправляет обновления всем подключенным клиентам
func handleMessages() {
    for {
        msg := <-broadcast
        mu.Lock()
        for client := range clients {
            err := client.WriteMessage(websocket.TextMessage, []byte(msg))
            if err != nil {
                log.Printf("Ошибка отправки сообщения клиенту: %v", err)
                client.Close()
                delete(clients, client)
            }
        }
        mu.Unlock()
    }
}

// startParser запускает цикл парсинга RSS и обновления БД каждые 5 минут
func startParser() {
    for {
        err := parseAndUpdate()
        if err != nil {
            log.Printf("Ошибка при парсинге или обновлении данных: %v", err)
        }

        time.Sleep(5 * time.Minute) // Парсить каждые 5 минут
    }
}

// parseAndUpdate выполняет парсинг RSS, обновляет базу данных и уведомляет клиентов
func parseAndUpdate() error {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL(rssURL)
    if err != nil {
        return err
    }

    tx, err := db.Begin()
    if err != nil {
        return err
    }

    stmt, err := tx.Prepare(`INSERT INTO ` + tableName + ` (title, link, description, pub_date)
        VALUES (?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE
            title = VALUES(title),
            link = VALUES(link),
            description = VALUES(description),
            pub_date = VALUES(pub_date)`)
    if err != nil {
        tx.Rollback()
        return err
    }
    defer stmt.Close()

    for _, item := range feed.Items {
        title := unidecode.Unidecode(item.Title)
        link := item.Link
        description := unidecode.Unidecode(item.Description)
        pubDate := item.PublishedParsed
        if pubDate == nil {
            pubDate = item.UpdatedParsed
        }
        pubDateStr := "Неизвестно"
        if pubDate != nil {
            pubDateStr = pubDate.Format("02.01.2006 15:04")
        }

        _, err := stmt.Exec(title, link, description, pubDateStr)
        if err != nil {
            log.Printf("Ошибка вставки записи: %v", err)
            continue
        }
    }

    err = tx.Commit()
    if err != nil {
        return err
    }

    // После обновления БД отправляем обновленные данные всем клиентам
    news, err := fetchNewsFromDB()
    if err != nil {
        return err
    }

    rendered, err := renderTemplate("parser", news)
    if err != nil {
        return err
    }

    broadcast <- rendered

    // Проверка, если все записи удалены, установить таймер на повторное добавление
    count, err := countNews()
    if err != nil {
        return err
    }

    if count == 0 {
        go func() {
            log.Println("Все записи удалены. Ожидание 1 минуты перед повторным добавлением...")
            time.Sleep(reconnectSec * time.Second)
            err := parseAndUpdate()
            if err != nil {
                log.Printf("Ошибка при повторном добавлении записей: %v", err)
            }
        }()
    }

    return nil
}

// fetchNewsFromDB извлекает все новости из базы данных
func fetchNewsFromDB() ([]NewsItem, error) {
    rows, err := db.Query(`SELECT title, link, description, pub_date FROM ` + tableName + ` ORDER BY pub_date DESC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var news []NewsItem
    for rows.Next() {
        var item NewsItem
        err := rows.Scan(&item.Title, &item.Link, &item.Description, &item.PubDate)
        if err != nil {
            return nil, err
        }
        news = append(news, item)
    }
    return news, nil
}

// renderTemplate рендерит шаблон parser.html для всех новостей и возвращает HTML строку
func renderTemplate(tmplName string, news []NewsItem) (string, error) {
    var rendered strings.Builder
    tmpl, err := template.ParseFiles("parser.html")
    if err != nil {
        return "", err
    }

    for _, item := range news {
        var sb strings.Builder
        err := tmpl.ExecuteTemplate(&sb, "parser", item)
        if err != nil {
            log.Printf("Ошибка рендеринга шаблона: %v", err)
            continue
        }
        rendered.WriteString(sb.String())
    }
    return rendered.String(), nil
}

// countNews возвращает количество новостей в базе данных
func countNews() (int, error) {
    var count int
    err := db.QueryRow(`SELECT COUNT(*) FROM ` + tableName).Scan(&count)
    return count, err
}
