<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>Новости</title>
    <script>
        var ws = new WebSocket("ws://" + window.location.host + "/ws");
        ws.onmessage = function(event) {
            var newsItems = JSON.parse(event.data);
            var content = "";
            newsItems.forEach(function(item) {
                content += `
                    <div class="news-item">
                        <h2><a href="${item.Link}" target="_blank">${item.Title}</a></h2>
                        <p>${item.Description}</p>
                        <small>${new Date(item.PubDate).toLocaleString('ru-RU')}</small>
                        <hr>
                    </div>
                `;
            });
            document.getElementById("content").innerHTML = content;
        };
        ws.onclose = function() {
            console.log("Соединение закрыто, повторная попытка...");
            setTimeout(function() {
                location.reload();
            }, 5000);
        };
    </script>
</head>
<body>
    <h1>Новости</h1>
    <div id="content"></div>
</body>
</html>
