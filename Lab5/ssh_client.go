// ssh_client.go
package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func main() {
	// Конфигурация SSH-клиента
	config := &ssh.ClientConfig{
		User: "testuser", // Имя пользователя (можно любое)
		Auth: []ssh.AuthMethod{
			ssh.Password("password123"), // Пароль для аутентификации
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Подключение к SSH-серверу
	client, err := ssh.Dial("tcp", "185.102.139.161:9742", config)
	if err != nil {
		log.Fatalf("Не удалось подключиться к SSH-серверу: %s", err)
	}
	defer client.Close()

	// Создание новой сессии
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Не удалось создать сессию: %s", err)
	}
	defer session.Close()

	// Получение файлового дескриптора терминала
	fd := int(os.Stdin.Fd())

	// Переключение терминала в сырой режим
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		log.Fatalf("Не удалось переключить терминал в сырой режим: %s", err)
	}
	defer term.Restore(fd, oldState)

	// Получение размеров терминала
	width, height, err := term.GetSize(fd)
	if err != nil {
		width = 80
		height = 24
	}

	// Запрос псевдотерминала
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Включить эхо
		ssh.TTY_OP_ISPEED: 14400, // Скорость ввода
		ssh.TTY_OP_OSPEED: 14400, // Скорость вывода
	}

	if err := session.RequestPty("xterm", height, width, modes); err != nil {
		log.Fatalf("Не удалось запросить псевдотерминал: %s", err)
	}

	// Настройка стандартных потоков ввода/вывода
	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	// Запуск shell
	if err := session.Shell(); err != nil {
		log.Fatalf("Не удалось запустить shell: %s", err)
	}

	// Ожидание завершения сессии
	if err := session.Wait(); err != nil {
		fmt.Printf("Сессия завершилась с ошибкой: %s\n", err)
	}
}
