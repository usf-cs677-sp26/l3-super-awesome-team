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
	"golang.org/x/sys/unix"
	"path/filepath"
)

func handleStorage(msgHandler *messages.MessageHandler, request *messages.StorageRequest) {
	log.Println("Attempting to store", request.FileName)

	// Ensure the file is not already present
	file, err := os.OpenFile(request.FileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666) // Create file if it doesn't exist, error if it does
	if err != nil {
		msgHandler.SendResponse(false, err.Error())
		msgHandler.Close()
		return
	}
	// We should check the space of disk before writing to it
	// If we send OK ("Ready for data"), the client will start streaming bytes immediately.
	dir := filepath.Dir(request.FileName)
	if dir == "" {
		dir = "."
	}
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		msgHandler.SendResponse(false, err.Error())
		msgHandler.Close()
		return
	}
	available := uint64(st.Bavail) * uint64(st.Bsize)
	if available < request.Size {
		msgHandler.SendResponse(false, fmt.Sprintf("not enough disk space: need=%d available=%d", request.Size, available))
		msgHandler.Close()
		return
	}

	msgHandler.SendResponse(true, "Ready for data") // Send OK response to client that we are ready to receive data
	md5 := md5.New()
	w := io.MultiWriter(file, md5)
	if _, err := io.CopyN(w, msgHandler, int64(request.Size)); err != nil { /* Write and checksum as we go */
		_ = file.Close()
		_ = os.Remove(request.FileName) // Don't keep partial/corrupt file
		_ = msgHandler.SendResponse(false, fmt.Sprintf("transfer failed while receiving data: %v", err))
		msgHandler.Close()
		return
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(request.FileName)
		_ = msgHandler.SendResponse(false, fmt.Sprintf("failed to close file: %v", err))
		msgHandler.Close()
		return
	}

	serverCheck := md5.Sum(nil)

	clientCheckMsg, err := msgHandler.Receive()
	if err != nil {
		_ = os.Remove(request.FileName)
		_ = msgHandler.SendResponse(false, fmt.Sprintf("failed to receive checksum: %v", err))
		msgHandler.Close()
		return
	}
	if clientCheckMsg.GetChecksum() == nil {
		_ = os.Remove(request.FileName)
		_ = msgHandler.SendResponse(false, "invalid checksum message")
		msgHandler.Close()
		return
	}
	clientCheck := clientCheckMsg.GetChecksum().Checksum

	if util.VerifyChecksum(serverCheck, clientCheck) {
		// log.Println("Successfully stored file.")
		_ = msgHandler.SendResponse(true, "Storage complete")
	} else {
		// log.Println("FAILED to store file. Invalid checksum.")
		_ = os.Remove(request.FileName)
		_ = msgHandler.SendResponse(false, "Invalid checksum")
	}

	// Requirement: disconnect client after responding with status.
	// msgHandler.Close()
}

func handleRetrieval(msgHandler *messages.MessageHandler, request *messages.RetrievalRequest) {
	log.Println("Attempting to retrieve", request.FileName)

	// Get file size and make sure it exists
	info, err := os.Stat(request.FileName)
	if err != nil {
		// Requirement: indicate failure if it doesn't exist (or can't stat).
		_ = msgHandler.SendRetrievalResponse(false, err.Error(), 0)
		msgHandler.Close()
		return
	}

	msgHandler.SendRetrievalResponse(true, "Ready to send", uint64(info.Size()))

	file, err := os.Open(request.FileName)
	if err != nil {
		_ = msgHandler.SendRetrievalResponse(false, err.Error(), 0)
		msgHandler.Close()
		return
	}

	md5 := md5.New()
	w := io.MultiWriter(msgHandler, md5)
	io.CopyN(w, file, info.Size()) // Checksum and transfer file at same time
	file.Close()

	checksum := md5.Sum(nil)
	msgHandler.SendChecksumVerification(checksum)
}

func handleClient(msgHandler *messages.MessageHandler) {
	defer msgHandler.Close()

	for {
		wrapper, err := msgHandler.Receive()
		if err != nil {
			log.Println(err)
		}

		switch msg := wrapper.Msg.(type) {
		case *messages.Wrapper_StorageReq:
			handleStorage(msgHandler, msg.StorageReq)
			continue
		case *messages.Wrapper_RetrievalReq:
			handleRetrieval(msgHandler, msg.RetrievalReq)
			continue
		case nil:
			log.Println("Received an empty message, terminating client")
			return
		default:
			log.Printf("Unexpected message type: %T", msg)
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
