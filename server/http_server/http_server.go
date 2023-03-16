package http_server

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/deepch/vdk/av/avutil"
	"github.com/deepch/vdk/format/flv"
	"github.com/general252/live/server/server_interface"
)

type HttpServer struct {
	parent server_interface.ServerInterface
	port   int
}

func NewHttpServer(parent server_interface.ServerInterface, port int) *HttpServer {
	return &HttpServer{
		parent: parent,
		port:   port,
	}
}

func (tis *HttpServer) Serve() error {
	// http
	http.HandleFunc("/", tis.handleHttpServer)

	addr := fmt.Sprintf(":%v", tis.port)
	log.Printf("http listen: %v", addr)

	return http.ListenAndServe(addr, nil)
}

// handleHttpServer http-flv
func (tis *HttpServer) handleHttpServer(w http.ResponseWriter, r *http.Request) {
	connPath := r.URL.Path
	ch, ok := tis.parent.GetChannel(connPath)

	if ok {
		w.Header().Set("Content-Type", "video/x-flv")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		flusher.Flush()

		muxer := flv.NewMuxerWriteFlusher(writeFlusher{httpFlusher: flusher, Writer: w})
		cursor := ch.Que.Latest()

		_ = avutil.CopyFile(muxer, cursor)
	} else {
		http.NotFound(w, r)
	}
}

type writeFlusher struct {
	httpFlusher http.Flusher
	io.Writer
}

func (c writeFlusher) Flush() error {
	c.httpFlusher.Flush()
	return nil
}
