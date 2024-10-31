// ssh_client.go
package main

import (
    "bufio"
    "fmt"
    "io"
    "log"
    "os"

    "golang.org/x/crypto/ssh"
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

    // Настройка стандартных потоков ввода/вывода
    session.Stdout = os.Stdout
    session.Stderr = os.Stderr

    // Создание каналов для обработки ввода
    stdin, err := session.StdinPipe()
    if err != nil {
        log.Fatalf("Не удалось настроить stdin: %s", err)
    }

    // Запрос псевдотерминала
    modes := ssh.TerminalModes{
        ssh.ECHO:          1,     // Включить эхо
        ssh.TTY_OP_ISPEED: 14400, // Скорость ввода
        ssh.TTY_OP_OSPEED: 14400, // Скорость вывода
    }

    if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
        log.Fatalf("Не удалось запросить псевдотерминал: %s", err)
    }

    // Запуск shell
    if err := session.Shell(); err != nil {
        log.Fatalf("Не удалось запустить shell: %s", err)
    }

    // Чтение ввода пользователя и отправка на сервер
    go func() {
        scanner := bufio.NewScanner(os.Stdin)
        for scanner.Scan() {
            line := scanner.Text() + "\n"
            _, err := io.WriteString(stdin, line)
            if err != nil {
                log.Printf("Ошибка записи в stdin: %s", err)
                break
            }
        }
    }()

    // Ожидание завершения сессии
    if err := session.Wait(); err != nil {
        log.Printf("Сессия завершилась с ошибкой: %s", err)
    }
}

