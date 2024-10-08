<!-- index.html -->
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>Одноранговая Сетевая Служба - Сообщения</title>
</head>
<body>
    <h1>Одноранговая Сетевая Служба - Сообщения</h1>

    <!-- Форма для отправки сообщений -->
    <div style="border: 2px solid blue; padding: 10px; margin: 10px;">
        <h2>Отправить сообщение</h2>
        <form id="sendMessageForm">
            <label for="sender">Отправитель:</label><br>
            <select id="sender" name="sender" required>
                <option value="">--Выберите отправителя--</option>
                <option value="Peer1">Peer1</option>
                <option value="Peer2">Peer2</option>
                <option value="Peer3">Peer3</option>
                <option value="Peer4">Peer4</option>
            </select><br><br>

            <label for="recipient">Получатель:</label><br>
            <select id="recipient" name="recipient" required>
                <option value="">--Выберите получателя--</option>
                <option value="Peer1">Peer1</option>
                <option value="Peer2">Peer2</option>
                <option value="Peer3">Peer3</option>
                <option value="Peer4">Peer4</option>
            </select><br><br>

            <label for="message">Сообщение:</label><br>
            <textarea id="message" name="message" rows="4" cols="50" required></textarea><br><br>

            <button type="submit">Отправить</button>
        </form>
        <div id="sendStatus" style="margin-top:10px;"></div>
    </div>

    <!-- Примеры GET-запросов для отправки сообщений -->
    <div style="border: 2px solid green; padding: 10px; margin: 10px;">
        <h2>Примеры отправки сообщений через GET-запросы</h2>
        <p>Вы можете отправить сообщение, перейдя по следующей ссылке (замените параметры по необходимости):</p>
        <ul>
            <li><a href="/send?from=Peer1&to=Peer2&msg=Hello">Отправить "Hello" от Peer1 к Peer2</a></li>
            <li><a href="/send?from=Peer3&to=Peer4&msg=Привет">Отправить "Привет" от Peer3 к Peer4</a></li>
        </ul>
    </div>

    <div id="peers">
        <div id="Peer1" style="border: 1px solid black; padding: 10px; margin: 10px;">
            <h2>Peer1 (185.104.251.226:9651)</h2>
            <div class="messages"></div>
        </div>
        <div id="Peer2" style="border: 1px solid black; padding: 10px; margin: 10px;">
            <h2>Peer2 (185.102.139.161:9651)</h2>
            <div class="messages"></div>
        </div>
        <div id="Peer3" style="border: 1px solid black; padding: 10px; margin: 10px;">
            <h2>Peer3 (185.102.139.168:9651)</h2> <!-- Исправлено -->
            <div class="messages"></div>
        </div>
        <div id="Peer4" style="border: 1px solid black; padding: 10px; margin: 10px;">
            <h2>Peer4 (185.102.139.169:9651)</h2> <!-- Исправлено -->
            <div class="messages"></div>
        </div>
    </div>

    <script>
        const peers = [
            { name: "Peer1", ip: "185.104.251.226", port: "9651" },
            { name: "Peer2", ip: "185.102.139.161", port: "9651" },
            { name: "Peer3", ip: "185.102.139.168", port: "9651" }, // Исправлено
            { name: "Peer4", ip: "185.102.139.169", port: "9651" }  // Исправлено
        ];

        peers.forEach(peer => {
            const ws = new WebSocket(`ws://${peer.ip}:${peer.port}/ws`);

            ws.onopen = () => {
                console.log(`Connected to ${peer.name} WebSocket`);
            };

            ws.onmessage = (event) => {
                const messageDiv = document.querySelector(`#${peer.name} .messages`);
                const msg = document.createElement('p');
                msg.textContent = event.data;
                messageDiv.appendChild(msg);
            };

            ws.onclose = () => {
                console.log(`Disconnected from ${peer.name} WebSocket`);
                // Попытка переподключения через 5 секунд
                setTimeout(() => {
                    reconnect(peer);
                }, 5000);
            };

            ws.onerror = (err) => {
                console.error(`WebSocket error with ${peer.name}:`, err);
                ws.close();
            };
        });

        function reconnect(peer) {
            const ws = new WebSocket(`ws://${peer.ip}:${peer.port}/ws`);

            ws.onopen = () => {
                console.log(`Reconnected to ${peer.name} WebSocket`);
            };

            ws.onmessage = (event) => {
                const messageDiv = document.querySelector(`#${peer.name} .messages`);
                const msg = document.createElement('p');
                msg.textContent = event.data;
                messageDiv.appendChild(msg);
            };

            ws.onclose = () => {
                console.log(`Disconnected from ${peer.name} WebSocket`);
                setTimeout(() => {
                    reconnect(peer);
                }, 5000);
            };

            ws.onerror = (err) => {
                console.error(`WebSocket error with ${peer.name}:`, err);
                ws.close();
            };
        }

        // Обработка отправки формы
        document.getElementById('sendMessageForm').addEventListener('submit', function(e) {
            e.preventDefault(); // Предотвращаем стандартное поведение формы

            const sender = document.getElementById('sender').value;
            const recipient = document.getElementById('recipient').value;
            const message = document.getElementById('message').value;

            if (!sender || !recipient || !message) {
                document.getElementById('sendStatus').innerText = "Все поля обязательны для заполнения.";
                return;
            }

            const payload = {
                sender: sender,
                recipient: recipient,
                message: message
            };

            fetch('/send', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(payload)
            })
            .then(response => response.json())
            .then(data => {
                if (data.status === 'success') {
                    document.getElementById('sendStatus').innerText = "Сообщение успешно отправлено.";
                    // Очистка формы
                    document.getElementById('sendMessageForm').reset();
                } else {
                    document.getElementById('sendStatus').innerText = "Ошибка при отправке сообщения.";
                }
            })
            .catch((error) => {
                console.error('Error:', error);
                document.getElementById('sendStatus').innerText = "Ошибка при отправке сообщения.";
            });
        });
    </script>
</body>
</html>
