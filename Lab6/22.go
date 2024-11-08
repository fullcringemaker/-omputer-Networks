<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>Новости</title>
</head>
<body>
    <h1>Последние новости</h1>
    <div id="news-container"></div>

    <script>
        var newsContainer = document.getElementById('news-container');
        var socket = new WebSocket("ws://localhost:9742/ws");

        socket.onmessage = function(event) {
            var newsItems = JSON.parse(event.data);
            newsContainer.innerHTML = '';
            newsItems.forEach(function(item) {
                var newsDiv = document.createElement('div');
                var title = document.createElement('h2');
                var link = document.createElement('a');
                link.href = item.Link;
                link.target = '_blank';
                link.textContent = item.Title;
                title.appendChild(link);
                var description = document.createElement('p');
                description.textContent = item.Description;
                var date = document.createElement('small');
                var dateObj = new Date(item.Date);
                date.textContent = dateObj.toLocaleString('ru-RU');
                newsDiv.appendChild(title);
                newsDiv.appendChild(description);
                newsDiv.appendChild(date);
                newsDiv.appendChild(document.createElement('hr'));
                newsContainer.appendChild(newsDiv);
            });
        };

        socket.onerror = function(error) {
            console.log('WebSocket Error: ' + error);
        };
    </script>
</body>
</html>
