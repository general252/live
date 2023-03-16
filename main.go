package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/general252/live/server"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	s := server.NewServer(&server.Option{
		RtmpPort: 1935,
		HttpPort: 8080,
		RtspPort: 554,
		RtpPort:  8000,
		RtcpPort: 8001,
	})

	s.Serve()

	quitChan := make(chan os.Signal, 2)
	signal.Notify(quitChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	<-quitChan

	// ffmpeg -re -i movie.flv -c copy -f flv rtmp://localhost/movie
	// ffmpeg -f avfoundation -i "0:0" .... -f flv rtmp://localhost/screen
	// ffplay http://localhost:8089/movie
	// ffplay http://localhost:8089/screen
}
