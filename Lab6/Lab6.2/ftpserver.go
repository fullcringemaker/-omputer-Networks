// ftpserver.go

package main

import (
	"log"
	"os"

	filedriver "github.com/goftp/file-driver"
	"github.com/goftp/server"
)

func main() {
	// Define the FTP server's root directory
	rootPath := "./ftproot"

	// Create the root directory if it doesn't exist
	err := os.MkdirAll(rootPath, os.ModePerm)
	if err != nil {
		log.Fatalf("Error creating root directory: %v", err)
	}

	// Set up the file driver (handles file system operations)
	factory := &filedriver.FileDriverFactory{
		RootPath: rootPath,
		Perm:     server.NewSimplePerm("user", "group"),
	}

	// Set up simple authentication with a username and password
	auth := &server.SimpleAuth{
		Name:     "user",     // Username
		Password: "password", // Password
	}

	// Configure the FTP server options
	opts := &server.ServerOpts{
		Factory:      factory,
		Auth:         auth,
		Port:         9742,          // FTP server port
		PassivePorts: "30000-30009", // Passive ports range
		Hostname:     "0.0.0.0",     // Bind to all network interfaces
	}

	// Create the FTP server instance
	ftpServer := server.NewServer(opts)

	// Start the FTP server
	log.Printf("Starting FTP server on port %d...", opts.Port)
	err = ftpServer.ListenAndServe()
	if err != nil {
		log.Fatalf("Error starting FTP server: %v", err)
	}
}
