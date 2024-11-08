package main

import (
    "database/sql"
    "fmt"
    "html/template"
    "log"
    "net/http"
    "sync"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/gorilla/websocket"
    "github.com/mmcdole/gofeed"
    "github.com/rainycape/unidecode"
)

var (
    clients   = make(map[*websocket.Conn]bool)
    broadcast = make(chan []NewsItem)
    upgrader  = websocket.Upgrader{}
    mutex     = &sync.Mutex{}
)

// Структура для новости
type NewsItem struct {
    ID          int
    Title       string
    Description string
    Date        time.Time
    Link        string
}

// Глобальная переменная для хранения последних новостей
var latestNews []NewsItem

func main() {
    // Запуск WebSocket-сервера
    http.HandleFunc("/ws", handleConnections)

    // Обслуживание dashboard
    http.HandleFunc("/", serveDashboard)

    // Обслуживание шаблона parser.html
    http.HandleFunc("/parser", serveParser)

    // Запуск фоновых процессов
    go handleMessages()
    go periodicUpdate()

    // Запуск сервера на порту 9742
    log.Println("Сервер запущен на :9742")
    err := http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("ListenAndServe: ", err)
    }
}

// Периодическое обновление новостей
func periodicUpdate() {
    for {
        updateNews()
        time.Sleep(10 * time.Second) // Интервал можно изменить по необходимости
    }
}

// Обновление новостей и базы данных
func updateNews() {
    // Парсинг RSS-ленты
    feedURL := "https://ldpr.ru/rss"
    feed, err := parseRSS(feedURL)
    if err != nil {
        log.Println("Ошибка при парсинге RSS:", err)
        return
    }

    // Подключение к базе данных
    db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs?charset=utf8")
    if err != nil {
        log.Println("Ошибка подключения к БД:", err)
        return
    }
    defer db.Close()

    if err = db.Ping(); err != nil {
        log.Println("Ошибка пинга БД:", err)
        return
    }

    // Получение существующих новостей из базы данных
    existingNews, err := fetchExistingNews(db)
    if err != nil {
        log.Println("Ошибка получения новостей из БД:", err)
        return
    }

    // Создание карты для быстрого доступа
    existingNewsMap := make(map[string]NewsItem)
    for _, news := range existingNews {
        existingNewsMap[news.Link] = news
    }

    // Вставка или обновление новостей
    for _, item := range feed.Items {
        // Используем unidecode для корректного отображения русских букв
        title := unidecode.Unidecode(item.Title)
        description := unidecode.Unidecode(item.Description)
        link := item.Link
        pubDate := item.PublishedParsed

        if pubDate == nil {
            continue
        }

        existingItem, exists := existingNewsMap[link]

        if !exists {
            // Вставка новой новости
            _, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)",
                title, description, pubDate.Format("2006-01-02 15:04:05"), link)
            if err != nil {
                log.Println("Ошибка при вставке новости:", err)
                continue
            }
        } else {
            // Обновление новости, если она изменилась
            if existingItem.Title != title || existingItem.Description != description {
                _, err := db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, date = ? WHERE id = ?",
                    title, description, pubDate.Format("2006-01-02 15:04:05"), existingItem.ID)
                if err != nil {
                    log.Println("Ошибка при обновлении новости:", err)
                    continue
                }
            }
        }
    }

    // Обновление списка новостей
    updatedNews, err := fetchExistingNews(db)
    if err != nil {
        log.Println("Ошибка получения обновленных новостей:", err)
        return
    }

    // Проверка на удаленные записи
    for _, existingItem := range existingNews {
        found := false
        for _, item := range feed.Items {
            if existingItem.Link == item.Link {
                found = true
                break
            }
        }
        if !found {
            // Повторная вставка удаленной записи
            _, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)",
                existingItem.Title, existingItem.Description, existingItem.Date.Format("2006-01-02 15:04:05"), existingItem.Link)
            if err != nil {
                log.Println("Ошибка при повторной вставке новости:", err)
                continue
            }
        }
    }

    // Обновление глобального списка новостей
    mutex.Lock()
    latestNews = updatedNews
    mutex.Unlock()

    // Отправка обновленных новостей всем клиентам
    broadcast <- updatedNews
}

// Парсинг RSS-ленты
func parseRSS(url string) (*gofeed.Feed, error) {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL(url)
    if err != nil {
        return nil, err
    }
    return feed, nil
}

// Получение существующих новостей из базы данных
func fetchExistingNews(db *sql.DB) ([]NewsItem, error) {
    rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko ORDER BY date DESC")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var newsItems []NewsItem
    for rows.Next() {
        var news NewsItem
        var dateStr string
        err := rows.Scan(&news.ID, &news.Title, &news.Description, &dateStr, &news.Link)
        if err != nil {
            return nil, err
        }
        news.Date, _ = time.Parse("2006-01-02 15:04:05", dateStr)
        newsItems = append(newsItems, news)
    }
    return newsItems, nil
}

// Обслуживание dashboard.html
func serveDashboard(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "dashboard.html")
}

// Обслуживание parser.html с использованием шаблона
func serveParser(w http.ResponseWriter, r *http.Request) {
    tmpl, err := template.ParseFiles("parser.html")
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Получение последних новостей
    mutex.Lock()
    newsItems := latestNews
    mutex.Unlock()

    // Отображение шаблона с данными новостей
    err = tmpl.Execute(w, newsItems)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
    }
}

// Обработка WebSocket-подключений
func handleConnections(w http.ResponseWriter, r *http.Request) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Ошибка WebSocket:", err)
        return
    }
    defer ws.Close()

    // Регистрация нового клиента
    clients[ws] = true

    // Отправка последних новостей новому клиенту
    mutex.Lock()
    initialNews := latestNews
    mutex.Unlock()
    ws.WriteJSON(initialNews)

    for {
        // Поддержание подключения
        _, _, err := ws.ReadMessage()
        if err != nil {
            log.Println("Ошибка чтения WebSocket:", err)
            delete(clients, ws)
            break
        }
    }
}

// Отправка обновлений новостей всем подключенным клиентам
func handleMessages() {
    for {
        news := <-broadcast
        for client := range clients {
            err := client.WriteJSON(news)
            if err != nil {
                log.Println("Ошибка записи WebSocket:", err)
                client.Close()
                delete(clients, client)
            }
        }
    }
}
