package main

import (
    "database/sql"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/gorilla/websocket"
    _ "github.com/go-sql-driver/mysql"
    "github.com/mmcdole/gofeed"
)

var (
    db          *sql.DB
    clients     = make(map[*websocket.Conn]bool)
    broadcast   = make(chan []NewsItem)
    rssFeedURL  = "http://rospotrebnadzor.ru/region/rss/rss.php"
    rssInterval = 5 * time.Minute
    upgrader    = websocket.Upgrader{}
)

// NewsItem представляет структуру новости
type NewsItem struct {
    ID          int
    Title       string
    Description string
    Date        string
}

func main() {
    var err error
    // Подключение к базе данных
    db, err = sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Проверка подключения
    err = db.Ping()
    if err != nil {
        log.Fatal(err)
    }

    // Запуск горутины для периодического парсинга RSS
    go func() {
        for {
            parseAndUpdateRSS()
            time.Sleep(rssInterval)
        }
    }()

    // Запуск горутины для рассылки обновлений
    go handleMessages()

    // Маршруты HTTP
    http.HandleFunc("/", serveHome)
    http.HandleFunc("/ws", handleConnections)

    fmt.Println("Сервер запущен на порту :8080")
    err = http.ListenAndServe(":8080", nil)
    if err != nil {
        log.Fatal("ListenAndServe: ", err)
    }
}

// Парсинг RSS и обновление базы данных
func parseAndUpdateRSS() {
    fp := gofeed.NewParser()
    feed, err := fp.ParseURL(rssFeedURL)
    if err != nil {
        log.Println("Ошибка парсинга RSS:", err)
        return
    }

    for _, item := range feed.Items {
        // Проверка наличия новости в базе данных
        var exists bool
        var id int
        err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE title = ?", item.Title).Scan(&id)
        if err != nil {
            if err == sql.ErrNoRows {
                exists = false
            } else {
                log.Println("Ошибка при проверке существования записи:", err)
                continue
            }
        } else {
            exists = true
        }

        if !exists {
            // Вставка новой новости
            _, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date) VALUES (?, ?, ?)",
                item.Title, item.Description, item.Published)
            if err != nil {
                log.Println("Ошибка вставки записи:", err)
                continue
            }
        } else {
            // Обновление новости, если она изменилась
            _, err := db.Exec("UPDATE iu9Trofimenko SET description = ?, date = ? WHERE id = ? AND (description != ? OR date != ?)",
                item.Description, item.Published, id, item.Description, item.Published)
            if err != nil {
                log.Println("Ошибка обновления записи:", err)
                continue
            }
        }
    }

    // Получение обновленных данных для отправки на клиент
    newsItems, err := getAllNews()
    if err != nil {
        log.Println("Ошибка получения новостей:", err)
        return
    }

    // Отправка обновлений всем подключенным клиентам
    broadcast <- newsItems
}

// Получение всех новостей из базы данных
func getAllNews() ([]NewsItem, error) {
    rows, err := db.Query("SELECT id, title, description, date FROM iu9Trofimenko")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var newsItems []NewsItem
    for rows.Next() {
        var item NewsItem
        err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Date)
        if err != nil {
            return nil, err
        }
        newsItems = append(newsItems, item)
    }
    return newsItems, nil
}

// Обработка подключений WebSocket
func handleConnections(w http.ResponseWriter, r *http.Request) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }
    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println(err)
        return
    }
    defer ws.Close()

    // Регистрация нового клиента
    clients[ws] = true

    // Отправка текущих новостей новому клиенту
    newsItems, err := getAllNews()
    if err != nil {
        log.Println("Ошибка получения новостей для нового клиента:", err)
        return
    }
    err = ws.WriteJSON(newsItems)
    if err != nil {
        log.Println("Ошибка отправки новостей новому клиенту:", err)
        return
    }

    // Ожидание закрытия соединения
    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            delete(clients, ws)
            break
        }
    }
}

// Рассылка обновлений всем подключенным клиентам
func handleMessages() {
    for {
        newsItems := <-broadcast
        for client := range clients {
            err := client.WriteJSON(newsItems)
            if err != nil {
                log.Printf("Клиент отключен: %v", err)
                client.Close()
                delete(clients, client)
            }
        }
    }
}

// Обслуживание главной страницы
func serveHome(w http.ResponseWriter, r *http.Request) {
    http.ServeFile(w, r, "index.html")
}

