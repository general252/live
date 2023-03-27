package httflv_server

import (
	"github.com/deepch/vdk/av/avutil"
	"github.com/deepch/vdk/format/flv"
	"github.com/general252/live/server/server_interface"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net/http"
)

type HttpFlvServer struct {
	parent server_interface.ServerInterface
}

func NewHttpFlvServer(parent server_interface.ServerInterface) *HttpFlvServer {
	return &HttpFlvServer{
		parent: parent,
	}
}

func (tis *HttpFlvServer) OnHttpFLV(c *gin.Context) {
	connPath := "/" + c.Param("ConnPath")
	log.Println(connPath)
	ch, ok := tis.parent.GetChannel(connPath)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{})
		return
	}

	var (
		isWebsocket = false
		ws          *websocketConnWrap
	)
	if len(c.Request.Header.Get("Sec-WebSocket-Key")) != 0 {
		var upgrade = websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		wsConn, err := upgrade.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"msg": err.Error(),
			})
			return
		}

		ws = &websocketConnWrap{conn: wsConn}
		isWebsocket = true
		defer wsConn.Close()
	}

	var (
		w        = c.Writer
		wFlusher = writeFlusher{
			httpFlusher: nil,
			Writer:      w,
		}
	)

	w.Header().Set("Access-Control-Allow-Origin", "*")

	if isWebsocket {
		wFlusher.Writer = ws
	} else {
		w.Header().Set("Content-Type", "video/x-flv")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(200)

		wFlusher.httpFlusher = w.(http.Flusher)
		wFlusher.httpFlusher.Flush()
	}

	muxer := flv.NewMuxerWriteFlusher(wFlusher)
	cursor := ch.Que.Latest()

	_ = avutil.CopyFile(muxer, cursor)
}

type websocketConnWrap struct {
	io.Writer
	conn *websocket.Conn
}

func (c *websocketConnWrap) Write(data []byte) (int, error) {
	err := c.conn.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		return 0, err
	}

	return len(data), nil
}

type writeFlusher struct {
	httpFlusher http.Flusher
	io.Writer
}

func (c writeFlusher) Flush() error {
	if c.httpFlusher != nil {
		c.httpFlusher.Flush()
	}
	return nil
}
