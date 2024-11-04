package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

func main() {
	server := ssh.Server{
		Addr: ":9742",
		Handler: func(s ssh.Session) {
			term := terminal.NewTerminal(s, "> ")
			for {
				line, err := term.ReadLine()
				if err != nil {
					if err == io.EOF {
						break
					}
					io.WriteString(s, "Error reading command: "+err.Error()+"\n")
					continue
				}
				args := strings.Fields(line)
				if len(args) == 0 {
					continue
				}
				switch args[0] {
				case "exit", "quit":
					io.WriteString(s, "Exit session.\n")
					return
				case "mkdir":
					if len(args) < 2 {
						io.WriteString(s, "Usage: mkdir <path>\n")
						continue
					}
					err := os.MkdirAll(args[1], 0755)
					if err != nil {
						io.WriteString(s, "Error creating directory: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "Directory created.\n")
					}
				case "rmdir":
					if len(args) < 2 {
						io.WriteString(s, "Usage: rmdir <path>\n")
						continue
					}
					err := os.RemoveAll(args[1])
					if err != nil {
						io.WriteString(s, "Error deleting directory: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "Directory deleted.\n")
					}
				case "ls":
					path := "."
					if len(args) > 1 {
						path = args[1]
					}
					files, err := os.ReadDir(path)
					if err != nil {
						io.WriteString(s, "Error reading directory: "+err.Error()+"\n")
						continue
					}
					for _, file := range files {
						io.WriteString(s, file.Name()+"\n")
					}
				case "mv":
					if len(args) < 3 {
						io.WriteString(s, "Usage: mv <source> <destination>\n")
						continue
					}
					err := os.Rename(args[1], args[2])
					if err != nil {
						io.WriteString(s, "Error moving file: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "The file has been moved.\n")
					}
				case "rm":
					if len(args) < 2 {
						io.WriteString(s, "Usage: rm <filename>\n")
						continue
					}
					err := os.Remove(args[1])
					if err != nil {
						io.WriteString(s, "Error deleting file: "+err.Error()+"\n")
					} else {
						io.WriteString(s, "The file has been deleted.\n")
					}
				case "ping":
					if len(args) < 2 {
						io.WriteString(s, "Usage: ping <address>\n")
						continue
					}
					cmd := exec.Command("ping", "-c", "4", args[1])
					output, err := cmd.CombinedOutput()
					if err != nil {
						io.WriteString(s, "Error executing ping: "+err.Error()+"\n")
					}
					io.WriteString(s, string(output))
				case "touch":
					if len(args) < 2 {
						io.WriteString(s, "Usage: touch <file path>\n")
						continue
					}
					file, err := os.Create(args[1])
					if err != nil {
						io.WriteString(s, "Error creating file: "+err.Error()+"\n")
						continue
					}
					file.Close()
					io.WriteString(s, "The file has been created.\n")
				default:
					io.WriteString(s, "Unknown command.\n")
				}
			}
		},
		PasswordHandler: func(ctx ssh.Context, password string) bool {
			log.Printf("Authorization attempt: user=%s password=%s\n", ctx.User(), password)
			return ctx.User() == "testuser" && password == "password123"
		},
	}
	log.Println("Running an SSH-server on port 9742...")
	log.Fatal(server.ListenAndServe())
}
