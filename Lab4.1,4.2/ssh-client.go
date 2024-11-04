package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func main() {
	config := &ssh.ClientConfig{
		User: "testuser",
		Auth: []ssh.AuthMethod{
			ssh.Password("password123"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	client, err := ssh.Dial("tcp", "185.102.139.168:9742", config)
	if err != nil {
		log.Fatalf("Failed to connect to SSH-server: %s", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("Failed to create session: %s", err)
	}
	defer session.Close()
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		log.Fatalf("Failed to switch terminal to raw mode: %s", err)
	}
	defer term.Restore(fd, oldState)
	width, height, err := term.GetSize(fd)
	if err != nil {
		width = 80
		height = 24
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", height, width, modes); err != nil {
		log.Fatalf("Failed to request pseudo-terminal: %s", err)
	}
	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	if err := session.Shell(); err != nil {
		log.Fatalf("Failed to start shell: %s", err)
	}
	if err := session.Wait(); err != nil {
		fmt.Printf("The session ended with an error: %s\n", err)
	}
}
