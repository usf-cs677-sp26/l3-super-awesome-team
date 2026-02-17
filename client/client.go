package main

import (
	"crypto/md5"
	"file-transfer/messages"
	"file-transfer/util"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"path/filepath"
)

func put(msgHandler *messages.MessageHandler, fileName string) int {
	fmt.Println("PUT", fileName)

	// Get file size and make sure it exists
	info, err := os.Stat(fileName)
	if err != nil {
		log.Println("Error stating file:", err)
		return 1
	}

	// Tell the server we want to store this file
	msgHandler.SendStorageRequest(filepath.Base(fileName), uint64(info.Size()))
	if ok, _ := msgHandler.ReceiveResponse(); !ok {
		return 1
	}

	file, err := os.Open(fileName)
	if err != nil {
		log.Println("Error opening file:", err)
		return 1
	}
	defer file.Close()

	h := md5.New()
	w := io.MultiWriter(msgHandler, h)
	if _, err := io.CopyN(w, file, info.Size()); err != nil {
		log.Println("Error transferring file:", err)
		return 1
	} // Checksum and transfer file at same time
	file.Close()

	checksum := h.Sum(nil)
	msgHandler.SendChecksumVerification(checksum)
	if ok, _ := msgHandler.ReceiveResponse(); !ok {
		return 1
	}

	fmt.Println("Storage complete!")
	return 0
}

func get(msgHandler *messages.MessageHandler, fileName string, dir string) int {
	fmt.Println("GET", fileName)

	msgHandler.SendRetrievalRequest(fileName)
	ok, _, size := msgHandler.ReceiveRetrievalResponse()
	if !ok {
		return 1
	}

	outputPath := filepath.Join(dir, fileName)
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer file.Close()

	h := md5.New()
	w := io.MultiWriter(file, h)
	if _, err := io.CopyN(w, msgHandler, int64(size)); err != nil {
		log.Println("Error receiving file:", err)
		return 1
	}
	file.Close()

	clientCheck := h.Sum(nil)
	checkMsg, err := msgHandler.Receive()
	if err != nil || checkMsg.GetChecksum() == nil {
		log.Println("Failed to receive checksum")
		return 1
	}
	serverCheck := checkMsg.GetChecksum().Checksum

	if util.VerifyChecksum(serverCheck, clientCheck) {
		log.Println("Successfully retrieved file.")
		return 0
	} else {
		log.Println("FAILED to retrieve file. Invalid checksum.")
		os.Remove(outputPath)
		return 1
	}

	return 0
}

func main() {
	if len(os.Args) < 4 {
		fmt.Printf("Not enough arguments. Usage: %s server:port put|get file-name [download-dir]\n", os.Args[0])
		os.Exit(1)
	}

	host := os.Args[1]
	conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Println("Error:", err)
		os.Exit(1)
	}
	msgHandler := messages.NewMessageHandler(conn)
	defer conn.Close()

	action := strings.ToLower(os.Args[2])
	if action != "put" && action != "get" {
		log.Println("Invalid action:", action)
        os.Exit(1)
	}

	fileName := os.Args[3]

	dir := "."
	if len(os.Args) >= 5 {
		dir = os.Args[4]
	}
	// check if dir exits
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		log.Printf("Directory %s does not exist\n", dir)
		os.Exit(1)
	}

	if action == "put" {
		os.Exit(put(msgHandler, fileName))
	} else if action == "get" {
		os.Exit(get(msgHandler, fileName))
	}
}
