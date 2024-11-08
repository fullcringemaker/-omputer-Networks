package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mmcdole/gofeed"
	"github.com/rainycape/unidecode"
	_ "github.com/go-sql-driver/mysql"
)

type NewsItem struct {
	Title       string
	Link        string
	Description string
	PubDate     time.Time
}

var (
	db          *sql.DB
	tmpl        *template.Template
	upgrader    = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	rssFeedURL  = "https://ldpr.ru/rss"
	updateDelay = time.Minute
)

func main() {
	var err error

	// Подключение к базе данных MySQL
	db, err = sql.Open("mysql", "iu9networkslabs:Je2dTYr6@tcp(students.yss.su)/iu9networkslabs")
	if err != nil {
		log.Fatal("Ошибка подключения к базе данных:", err)
	}
	defer db.Close()

	// Парсим шаблон parser.html
	tmpl, err = template.ParseFiles("parser.html")
	if err != nil {
		log.Fatal("Ошибка чтения parser.html:", err)
	}

	// Запуск обновления RSS
	go rssUpdater()

	// Настройка HTTP сервера
	http.HandleFunc("/", serveDashboard)
	http.HandleFunc("/ws", handleWebSocket)

	log.Println("Сервер запущен на порту 9742...")
	log.Fatal(http.ListenAndServe(":9742", nil))
}

// Обработчик WebSocket
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка WebSocket:", err)
		return
	}
	defer conn.Close()

	// Устанавливаем интервал обновления
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			news, err := fetchNewsFromDB()
			if err != nil {
				log.Println("Ошибка получения новостей из БД:", err)
				return
			}

			htmlContent, err := renderHTML(news)
			if err != nil {
				log.Println("Ошибка рендеринга HTML:", err)
				return
			}

			err = conn.WriteMessage(websocket.TextMessage, []byte(htmlContent))
			if err != nil {
				log.Println("Ошибка отправки сообщения по WebSocket:", err)
				return
			}
		}
	}
}

func rssUpdater() {
	for {
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(rssFeedURL)
		if err != nil {
			log.Println("Ошибка парсинга RSS:", err)
			time.Sleep(updateDelay)
			continue
		}

		for _, item := range feed.Items {
			title := unidecode.Unidecode(item.Title)
			link := item.Link
			description := unidecode.Unidecode(item.Description)
			pubDate, err := time.Parse(time.RFC1123Z, item.Published)
			if err != nil {
				log.Println("Ошибка парсинга даты из RSS:", err)
				continue
			}

			_, err = db.Exec(`INSERT INTO iu9Trofimenko (title, link, description, pub_date)
				VALUES (?, ?, ?, ?)
				ON DUPLICATE KEY UPDATE
					title = VALUES(title),
					link = VALUES(link),
					description = VALUES(description),
					pub_date = VALUES(pub_date)`,
				title, link, description, pubDate.Format("2006-01-02 15:04:05"))
			if err != nil {
				log.Println("Ошибка обновления базы данных:", err)
			}
		}

		checkAndRestoreNews()
		time.Sleep(updateDelay)
	}
}

func checkAndRestoreNews() {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM iu9Trofimenko").Scan(&count)
	if err != nil {
		log.Println("Ошибка проверки записей:", err)
		return
	}
	if count == 0 {
		log.Println("Таблица пуста, восстанавливаем через минуту...")
		time.Sleep(time.Minute)
		rssUpdater()
	}
}

func fetchNewsFromDB() ([]NewsItem, error) {
	rows, err := db.Query("SELECT title, link, description, pub_date FROM iu9Trofimenko ORDER BY pub_date DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var news []NewsItem
	for rows.Next() {
		var item NewsItem
		var pubDateStr string

		if err := rows.Scan(&item.Title, &item.Link, &item.Description, &pubDateStr); err != nil {
			return nil, err
		}

		pubDate, err := time.Parse("2006-01-02 15:04:05", pubDateStr)
		if err != nil {
			item.PubDate = time.Now()
		} else {
			item.PubDate = pubDate
		}

		news = append(news, item)
	}
	return news, nil
}

func renderHTML(news []NewsItem) (string, error) {
	var htmlContent string
	writer := ioutil.Discard
	err := tmpl.Execute(writer, news)
	if err != nil {
		return "", err
	}
	return htmlContent, nil
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "dashboard.html")
}
