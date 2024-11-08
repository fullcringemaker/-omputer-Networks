<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>Новости</title>
</head>
<body>
    <div id="news-container">
        {{template "parser.html" .}}
    </div>

    <script>
        var ws = new WebSocket("ws://" + location.host + "/ws");

        ws.onmessage = function(event) {
            var newsItems = JSON.parse(event.data);
            var container = document.getElementById("news-container");
            container.innerHTML = "";

            newsItems.forEach(function(item) {
                var div = document.createElement("div");
                div.className = "news-item";

                var title = document.createElement("h2");
                var titleLink = document.createElement("a");
                titleLink.href = item.Link;
                titleLink.target = "_blank";
                titleLink.textContent = item.Title;
                title.appendChild(titleLink);
                div.appendChild(title);

                var description = document.createElement("p");
                description.textContent = item.Description;
                div.appendChild(description);

                var date = document.createElement("small");
                date.textContent = item.Date;
                div.appendChild(date);

                var hr = document.createElement("hr");
                div.appendChild(hr);

                container.appendChild(div);
            });
        };
    </script>
</body>
</html>
