package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func proxyHandler(response http.ResponseWriter, request *http.Request) {
	// Получение домена из URL
	domain := strings.TrimPrefix(request.URL.Path, "/")
	if !strings.HasPrefix(domain, "http") {
		domain = "https://" + domain
	}

	// Создание запроса для целевого сервера
	reqProxy, err := http.NewRequest(request.Method, domain, request.Body)
	if err != nil {
		http.Error(response, "Invalid request", http.StatusBadRequest)
		return
	}

	// Перенос заголовков из исходного запроса
	reqProxy.Header = request.Header

	// Настройка клиента для пропуска проверки сертификата
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Выполнение запроса к целевому серверу
	respProxy, err := httpClient.Do(reqProxy)
	if err != nil {
		http.Error(response, "Unable to reach target site", http.StatusBadGateway)
		return
	}
	defer respProxy.Body.Close()

	// Обработка и модификация HTML-ответа
	if strings.Contains(respProxy.Header.Get("Content-Type"), "text/html") {
		processHTML(response, respProxy, request.Host)
	} else {
		// Проксирование прочих файлов (CSS, JS, изображения)
		copyNonHTMLContent(response, respProxy)
	}
}

// Обработка и замена ссылок в HTML
func processHTML(w http.ResponseWriter, r *http.Response, proxyDomain string) {
	doc, err := goquery.NewDocumentFromReader(r.Body)
	if err != nil {
		http.Error(w, "Error parsing HTML", http.StatusInternalServerError)
		return
	}

	// Проксирование гиперссылок
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, exists := s.Attr("href"); exists && strings.HasPrefix(href, "http") {
			hrefWithoutProtocol := strings.TrimPrefix(href, "https://")
			hrefWithoutProtocol = strings.TrimPrefix(hrefWithoutProtocol, "http://")
			proxyHref := "https://" + proxyDomain + "/" + hrefWithoutProtocol
			fmt.Println(proxyHref)
			s.SetAttr("href", proxyHref)
		}
	})

	// Проксирование изображений
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists {
			proxySrc := "https://" + proxyDomain + "/" + r.TLS.ServerName + "/" + src
			fmt.Println(proxySrc)
			s.SetAttr("src", proxySrc)
		}
	})

	// Возвращение модифицированного HTML
	modifiedHTML, err := doc.Html()
	if err != nil {
		http.Error(w, "Error generating HTML", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(r.StatusCode)
	w.Write([]byte(modifiedHTML))
}

// Копирование контента, отличного от HTML
func copyNonHTMLContent(w http.ResponseWriter, r *http.Response) {
	for headerName, headerValues := range r.Header {
		w.Header()[headerName] = headerValues
	}
	w.WriteHeader(r.StatusCode)
	io.Copy(w, r.Body)
}

func main() {
	// Запуск прокси-сервера на порту 9742
	http.HandleFunc("/", proxyHandler)
	log.Println("Proxy server is running on port 9742")
	log.Fatal(http.ListenAndServeTLS(":9742", "server.crt", "server.key", nil))
}
