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
	"path/filepath"
	"golang.org/x/sys/unix"
)

func handleStorage(msgHandler *messages.MessageHandler, request *messages.StorageRequest) {
	log.Println("Attempting to store", request.FileName)
	file, err := os.OpenFile(request.FileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		msgHandler.SendResponse(false, err.Error())
		msgHandler.Close()
		return
	}
	defer file.Close()
    
	// check disk space
    wd, _ := os.Getwd()
	var stat unix.Statfs_t
	if err := unix.Statfs(wd, &stat); err != nil {
		msgHandler.SendResponse(false, "Internal server error")
		msgHandler.Close()
		return
	}
	availableSpace := stat.Bavail * uint64(stat.Bsize)
	if request.Size > availableSpace {
		msgHandler.SendResponse(false, "Not enough disk space")
		file.Close()
		os.Remove(request.FileName)
		msgHandler.Close()
		return
	}
    
	// send an “OK” response to the client so it knows it can begin sending the file
	msgHandler.SendResponse(true, "Ready for data")

	//Receive data stream and store the file
	h := md5.New()
	w := io.MultiWriter(file, h)
	if _, err := io.CopyN(w, msgHandler, int64(request.Size)); err != nil {
		file.Close()
		os.Remove(request.FileName)
		msgHandler.Close()
		return
	}
	file.Close()

	// Verify its checksum against the checksum sent by the client
	serverCheck := h.Sum(nil)
	clientCheckMsg, err := msgHandler.Receive()
    if err != nil || clientCheckMsg.GetChecksum() == nil {
		os.Remove(request.FileName)
		msgHandler.Close()
		return
	}
	clientCheck := clientCheckMsg.GetChecksum().Checksum

	// Respond to the client with the status of the transfer (success or failure)
	if util.VerifyChecksum(serverCheck, clientCheck) {
		log.Println("Successfully stored file.")
		msgHandler.SendResponse(true, "Storage complete")
	} else {
		log.Println("FAILED to store file. Invalid checksum.")
		os.Remove(request.FileName)
		msgHandler.SendResponse(false, "Invalid checksum")
	}

	// Disconnect the client
	msgHandler.Close()
}

func handleRetrieval(msgHandler *messages.MessageHandler, request *messages.RetrievalRequest) {
	log.Println("Attempting to retrieve", request.FileName)

	// Get file size and make sure it exists
	info, err := os.Stat(request.FileName)
	if err != nil {
		msgHandler.SendRetrievalResponse(false, "File not found", 0)
		msgHandler.Close()
		return
	}
    
	// Send a response back to the client with the file’s size and checksum
	msgHandler.SendRetrievalResponse(true, "Ready to send", uint64(info.Size()))

	file, err := os.Open(request.FileName)
	if err != nil {
		_ = msgHandler.SendRetrievalResponse(false, err.Error(), 0)
		msgHandler.Close()
		return
	}
	defer file.Close()

	// Begin streaming file to the client
	h := md5.New()
	w := io.MultiWriter(msgHandler, h)
	io.CopyN(w, file, info.Size()) // Checksum and transfer file at same time
	file.Close()

	checksum := h.Sum(nil)
	msgHandler.SendChecksumVerification(checksum)

	// Disconnect the client
	msgHandler.Close()
}

func handleClient(msgHandler *messages.MessageHandler) {
	defer msgHandler.Close()

	for {
		wrapper, err := msgHandler.Receive()
		if err != nil {
			return 
		}

		switch msg := wrapper.Msg.(type) {
		case *messages.Wrapper_StorageReq:
			handleStorage(msgHandler, msg.StorageReq)
			return
		case *messages.Wrapper_RetrievalReq:
			handleRetrieval(msgHandler, msg.RetrievalReq)
			return 
		case nil:
			log.Println("Received an empty message, terminating client")
			return
		default:
			log.Printf("Unexpected message type: %T", msg)
			return
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Not enough arguments. Usage: %s port [download-dir]\n", os.Args[0])
		os.Exit(1)
	}

	port := os.Args[1]
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalln(err.Error())
		os.Exit(1)
	}
	defer listener.Close()

	dir := "."
	if len(os.Args) >= 3 {
		dir = os.Args[2]
	}
	if err := os.Chdir(dir); err != nil {
		log.Fatalln(err)
	}

	fmt.Println("Listening on port:", port)
	fmt.Println("Download directory:", dir)
	for {
		if conn, err := listener.Accept(); err == nil {
			log.Println("Accepted connection", conn.RemoteAddr())
			handler := messages.NewMessageHandler(conn)
			go handleClient(handler)
		}
	}
}
