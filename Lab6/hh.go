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
    "github.com/SlyMarbo/rss"
    _ "github.com/go-sql-driver/mysql"
)

type NewsItem struct {
    ID          int    `json:"id"`
    Title       string `json:"title"`
    Description string `json:"description"`
    Date        string `json:"date"`
    Link        string `json:"link"`
}

var (
    db            *sql.DB
    newsItems     []NewsItem
    newsItemsLock sync.Mutex

    clients     = make(map[*websocket.Conn]bool)
    clientsLock sync.Mutex
    broadcast   = make(chan []NewsItem)
    upgrader    = websocket.Upgrader{}
)

func main() {
    var err error
    // Подключение к базе данных
    db, err = sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Создание таблицы, если не существует
    err = createTableIfNotExists()
    if err != nil {
        log.Fatal(err)
    }

    // Парсинг RSS и обновление базы данных
    err = updateNewsFromRSS()
    if err != nil {
        log.Println("Ошибка обновления новостей из RSS:", err)
    }

    // Загрузка новостей из базы данных
    err = loadNewsItemsFromDB()
    if err != nil {
        log.Fatal(err)
    }

    // Настройка маршрутов HTTP-сервера
    http.HandleFunc("/", handleDashboard)
    http.HandleFunc("/ws", handleConnections)

    // Запуск горутины для рассылки обновлений
    go handleBroadcast()

    // Запуск периодических задач
    go startPeriodicTasks()

    log.Println("Сервер запущен на :9742")
    err = http.ListenAndServe(":9742", nil)
    if err != nil {
        log.Fatal("ListenAndServe: ", err)
    }
}

func createTableIfNotExists() error {
    query := `
    CREATE TABLE IF NOT EXISTS iu9Trofimenko (
        id INT AUTO_INCREMENT PRIMARY KEY,
        title VARCHAR(255),
        description TEXT,
        date VARCHAR(10),
        link VARCHAR(255)
    )
    `
    _, err := db.Exec(query)
    return err
}

func updateNewsFromRSS() error {
    feed, err := rss.Fetch("https://rospotrebnadzor.ru/region/rss/rss.php?rss=y")
    if err != nil {
        return err
    }

    for _, item := range feed.Items {
        pubDate := item.Date.Format("02.01.2006")
        link := item.Link

        var exists bool
        err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM iu9Trofimenko WHERE link = ?)", link).Scan(&exists)
        if err != nil {
            log.Println("Ошибка проверки существования записи:", err)
            continue
        }

        if exists {
            var dbTitle, dbDescription string
            err = db.QueryRow("SELECT title, description FROM iu9Trofimenko WHERE link = ?", link).Scan(&dbTitle, &dbDescription)
            if err != nil {
                log.Println("Ошибка получения записи:", err)
                continue
            }
            if dbTitle != item.Title || dbDescription != item.Summary {
                _, err = db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, date = ? WHERE link = ?", item.Title, item.Summary, pubDate, link)
                if err != nil {
                    log.Println("Ошибка обновления записи:", err)
                    continue
                }
            }
        } else {
            _, err = db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link) VALUES (?, ?, ?, ?)", item.Title, item.Summary, pubDate, link)
            if err != nil {
                log.Println("Ошибка вставки новой записи:", err)
                continue
            }
        }
    }

    return nil
}

func loadNewsItemsFromDB() error {
    rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko")
    if err != nil {
        return err
    }
    defer rows.Close()

    var items []NewsItem
    for rows.Next() {
        var item NewsItem
        err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Date, &item.Link)
        if err != nil {
            return err
        }
        items = append(items, item)
    }
    newsItemsLock.Lock()
    newsItems = items
    newsItemsLock.Unlock()
    return nil
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
    tmpl := template.Must(template.ParseFiles("dashboard.html"))
    err := tmpl.Execute(w, nil)
    if err != nil {
        http.Error(w, "Ошибка отображения шаблона", http.StatusInternalServerError)
        return
    }
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }

    ws, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Println("Ошибка обновления до вебсокета:", err)
        return
    }
    defer ws.Close()

    clientsLock.Lock()
    clients[ws] = true
    clientsLock.Unlock()

    // Отправка начальных данных клиенту
    newsItemsLock.Lock()
    err = ws.WriteJSON(newsItems)
    newsItemsLock.Unlock()
    if err != nil {
        log.Println("Ошибка отправки начальных данных:", err)
    }

    // Прослушивание сообщений от клиента (если необходимо)
    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            clientsLock.Lock()
            delete(clients, ws)
            clientsLock.Unlock()
            break
        }
    }
}

func handleBroadcast() {
    for {
        items := <-broadcast
        clientsLock.Lock()
        for client := range clients {
            err := client.WriteJSON(items)
            if err != nil {
                log.Printf("Ошибка рассылки клиенту: %v", err)
                client.Close()
                delete(clients, client)
            }
        }
        clientsLock.Unlock()
    }
}

func startPeriodicTasks() {
    // Периодическое обновление новостей из RSS
    rssTicker := time.NewTicker(5 * time.Minute)
    go func() {
        for {
            <-rssTicker.C
            err := updateNewsFromRSS()
            if err != nil {
                log.Println("Ошибка обновления новостей из RSS:", err)
            }
        }
    }()

    // Периодическая проверка изменений в базе данных
    dbTicker := time.NewTicker(10 * time.Second)
    go func() {
        for {
            <-dbTicker.C
            var updated bool
            var newItems []NewsItem
            // Загрузка новостей из базы данных
            rows, err := db.Query("SELECT id, title, description, date, link FROM iu9Trofimenko")
            if err != nil {
                log.Println("Ошибка запроса к базе данных:", err)
                continue
            }
            defer rows.Close()

            for rows.Next() {
                var item NewsItem
                err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Date, &item.Link)
                if err != nil {
                    log.Println("Ошибка сканирования строки:", err)
                    continue
                }
                newItems = append(newItems, item)
            }

            newsItemsLock.Lock()
            if !newsItemsEqual(newsItems, newItems) {
                newsItems = newItems
                updated = true
            }
            newsItemsLock.Unlock()

            if updated {
                // Рассылка нового списка клиентам
                broadcast <- newItems
            }
        }
    }()
}

func newsItemsEqual(a, b []NewsItem) bool {
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
