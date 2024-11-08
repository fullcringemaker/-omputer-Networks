<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>Dashboard Новостей</title>
</head>
<body>
    <h1>Новости</h1>
    <div id="news-container">
        <!-- Новости будут отображаться здесь -->
    </div>

    <script>
        const ws = new WebSocket("ws://localhost:9742/ws");

        ws.onopen = function() {
            console.log("Подключено к WebSocket");
        };

        ws.onmessage = function(event) {
            const news = JSON.parse(event.data);
            const container = document.getElementById("news-container");
            container.innerHTML = ""; // Очистка контейнера

            news.forEach(item => {
                const newsItem = document.createElement("div");
                newsItem.className = "news-item";

                const title = document.createElement("h2");
                const link = document.createElement("a");
                link.href = item.link;
                link.target = "_blank";
                link.textContent = item.title || "Без заголовка";
                title.appendChild(link);

                const description = document.createElement("p");
                description.textContent = item.description || "Без описания";

                const pubDate = document.createElement("small");
                pubDate.textContent = item.pub_date || "Неизвестная дата";

                const hr = document.createElement("hr");

                newsItem.appendChild(title);
                newsItem.appendChild(description);
                newsItem.appendChild(pubDate);
                newsItem.appendChild(hr);

                container.appendChild(newsItem);
            });
        };

        ws.onclose = function() {
            console.log("WebSocket отключен");
        };

        ws.onerror = function(error) {
            console.log("WebSocket ошибка:", error);
        };
    </script>
</body>
</html>
