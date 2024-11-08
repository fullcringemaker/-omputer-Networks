package main

import (
	"database/sql"
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

type NewsItem struct {
	ID          int
	Title       string
	Description string
	Date        string
	Link        string
	Author      string
	Content     string
}

var (
	clients      = make(map[*websocket.Conn]bool)
	clientsMutex sync.Mutex
	broadcast    = make(chan []NewsItem)
	upgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

func main() {
	// Подключение к базе данных с правильной кодировкой
	db, err := sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs?charset=utf8mb4&parseTime=true&loc=Local")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Установка кодировки соединения
	_, err = db.Exec("SET NAMES 'utf8mb4'")
	if err != nil {
		log.Fatal(err)
	}

	// Парсинг шаблонов
	tmplDashboard := template.Must(template.ParseFiles("dashboard.html", "parser.html"))

	// Запуск broadcaster
	go broadcaster()

	// Обработка маршрутов
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		newsItems, err := getAllNewsItems(db)
		if err != nil {
			log.Printf("Ошибка при получении новостей: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		tmplDashboard.Execute(w, newsItems)
	})

	http.HandleFunc("/ws", handleConnections)

	// Запуск функции обновления RSS и базы данных
	go func() {
		for {
			err := fetchAndUpdateRSS(db)
			if err != nil {
				log.Printf("Ошибка при получении RSS: %v", err)
			}
			time.Sleep(1 * time.Minute)
		}
	}()

	// Запуск мониторинга базы данных
	go monitorDatabase(db)

	// Запуск сервера
	log.Println("Сервер запущен на порту :9742")
	err = http.ListenAndServe(":9742", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func fetchAndUpdateRSS(db *sql.DB) error {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://ldpr.ru/rss")
	if err != nil {
		return err
	}

	for _, item := range feed.Items {
		// Используем unidecode для обработки русских букв
		title := unidecode.Unidecode(item.Title)
		description := unidecode.Unidecode(item.Description)
		content := unidecode.Unidecode(item.Content)
		link := item.Link
		author := ""
		if item.Author != nil {
			author = unidecode.Unidecode(item.Author.Name)
		}
		pubDate := item.PublishedParsed

		date := ""
		if pubDate != nil {
			date = pubDate.Format("02.01.2006")
		}

		// Проверяем, существует ли запись в базе данных
		var existingID int
		err := db.QueryRow("SELECT id FROM iu9Trofimenko WHERE link = ?", link).Scan(&existingID)
		if err != nil {
			if err == sql.ErrNoRows {
				// Новая запись, вставляем в базу данных
				_, err := db.Exec("INSERT INTO iu9Trofimenko (title, description, date, link, author, content) VALUES (?, ?, ?, ?, ?, ?)",
					title, description, date, link, author, content)
				if err != nil {
					log.Printf("Ошибка при вставке записи: %v", err)
					continue
				}
			} else {
				log.Printf("Ошибка при запросе к базе данных: %v", err)
				continue
			}
		} else {
			// Запись существует, проверяем на изменения
			var dbTitle, dbDescription, dbContent, dbAuthor string
			err := db.QueryRow("SELECT title, description, content, author FROM iu9Trofimenko WHERE link = ?", link).Scan(&dbTitle, &dbDescription, &dbContent, &dbAuthor)
			if err != nil {
				log.Printf("Ошибка при получении существующей записи: %v", err)
				continue
			}

			if dbTitle != title || dbDescription != description || dbContent != content || dbAuthor != author {
				// Обновляем запись
				_, err := db.Exec("UPDATE iu9Trofimenko SET title = ?, description = ?, content = ?, author = ? WHERE link = ?",
					title, description, content, author, link)
				if err != nil {
					log.Printf("Ошибка при обновлении записи: %v", err)
					continue
				}
			}
		}
	}

	return nil
}

func getAllNewsItems(db *sql.DB) ([]NewsItem, error) {
	rows, err := db.Query("SELECT id, title, description, date, link, author, content FROM iu9Trofimenko ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newsItems []NewsItem
	for rows.Next() {
		var item NewsItem
		err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Date, &item.Link, &item.Author, &item.Content)
		if err != nil {
			return nil, err
		}
		newsItems = append(newsItems, item)
	}
	return newsItems, nil
}

func monitorDatabase(db *sql.DB) {
	var lastNewsItems []NewsItem
	for {
		newsItems, err := getAllNewsItems(db)
		if err != nil {
			log.Printf("Ошибка при мониторинге базы данных: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if !newsItemsEqual(newsItems, lastNewsItems) {
			broadcast <- newsItems
			lastNewsItems = newsItems
		}

		time.Sleep(2 * time.Second)
	}
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

func broadcaster() {
	for {
		news := <-broadcast
		clientsMutex.Lock()
		for client := range clients {
			err := client.WriteJSON(news)
			if err != nil {
				log.Printf("Ошибка при отправке данных клиенту: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
		clientsMutex.Unlock()
	}
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Обновление соединения до веб-сокета
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Ошибка при обновлении до веб-сокета: %v", err)
		return
	}
	// Регистрация нового клиента
	clientsMutex.Lock()
	clients[ws] = true
	clientsMutex.Unlock()

	// Прослушивание сообщений от клиента
	go func() {
		defer func() {
			clientsMutex.Lock()
			delete(clients, ws)
			clientsMutex.Unlock()
			ws.Close()
		}()

		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}
