package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/SlyMarbo/rss"
)

// Обработчик для главной страницы
func HomeRouterHandler(w http.ResponseWriter, r *http.Request) {
	// Получаем RSS-канал
	rssObject, err := rss.Fetch("https://news.rambler.ru/rss/Magadan/")
	if err != nil {
		fmt.Println(err)
		panic("failed")
	}

	// Выводим заголовок и описание RSS
	fmt.Fprintf(w, "<h1>%s</h1>\n", rssObject.Title)
	fmt.Fprintf(w, "<p>%s</p>\n", rssObject.Description)

	// Выводим список новостей
	for i, item := range rssObject.Items {
		fmt.Fprintf(w, "<p>%d) %s - <a href=\"%s\">%s</a></p>\n", i+1, item.Title, item.Link, item.Link)
	}
}

func main() {
	http.HandleFunc("/", HomeRouterHandler)  // Устанавливаем роутер
	err := http.ListenAndServe(":9000", nil) // Задаем слушать порт
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
