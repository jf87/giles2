package websocket

import (
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/websocket"
	giles "github.com/jf87/giles2/archiver"
	"github.com/jf87/giles2/common"
	"github.com/julienschmidt/httprouter"
	"github.com/op/go-logging"
)

// logger
var log *logging.Logger

// set up logging facilities
func init() {
	log = logging.MustGetLogger("websocket")
	var format = "%{color}%{level} %{time:Jan 02 15:04:05} %{shortfile}%{color:reset} ▶ %{message}"
	var logBackend = logging.NewLogBackend(os.Stderr, "", 0)
	logBackendLeveled := logging.AddModuleLevel(logBackend)
	logging.SetBackend(logBackendLeveled)
	logging.SetFormatter(logging.MustStringFormatter(format))
}

var upgrader = &websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type WebSocketHandler struct {
	a       *giles.Archiver
	handler http.Handler
}

func NewWebSocketHandler(a *giles.Archiver) *WebSocketHandler {
	r := httprouter.New()
	h := &WebSocketHandler{a, r}
	r.GET("/add/:key", h.handleAdd)
	r.GET("/republish", h.handleRepublish)

	go m.start()

	return h
}

func Handle(a *giles.Archiver, port int) {
	h := NewWebSocketHandler(a)

	address, err := net.ResolveTCPAddr("tcp4", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		log.Fatalf("Error resolving address %v: %v", "0.0.0.0:"+strconv.Itoa(port), err)
	}

	log.Noticef("Starting WebSockets on %v", address.String())
	srv := &http.Server{
		Addr:    address.String(),
		Handler: h.handler,
	}
	srv.ListenAndServe()
}

func (h *WebSocketHandler) handleAdd(rw http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var (
		messages common.TieredSmapMessage
		err      error
	)
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Access-Control-Allow-Origin", "*")
	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		log.Errorf("Error establishing websocket: %v", err)
		return
	}

	for {
		err = ws.ReadJSON(messages)
		if err != nil {
			log.Errorf("Error reading JSON: %v", err)
			ws.Close()
			return
		}
	}

}

func (h *WebSocketHandler) handleRepublish(rw http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var (
		//messages common.TieredSmapMessage
		err error
	)
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Access-Control-Allow-Origin", "*")
	ws, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		log.Errorf("Error establishing websocket: %v", err)
		return
	}
	msgtype, msg, err := ws.ReadMessage()
	log.Debugf("msgtype: %v, msg: %v, err: %v", msgtype, string(msg), err)

	subscription := StartSubscriber(ws)
	h.a.HandleNewSubscriber(subscription, "select * where "+string(msg))
}

type manager struct {
	// registered connections
	subscribers map[*WebSocketSubscriber]bool

	// new connection request
	initialize chan *WebSocketSubscriber

	// get rid of old connections
	remove chan *WebSocketSubscriber
}

var m = manager{
	subscribers: make(map[*WebSocketSubscriber]bool),
	initialize:  make(chan *WebSocketSubscriber),
	remove:      make(chan *WebSocketSubscriber),
}

func (m *manager) start() {
	for {
		select {
		case wss := <-m.initialize:
			m.subscribers[wss] = true
		case wss := <-m.remove:
			if _, found := m.subscribers[wss]; found {
				wss.closeC <- true
				wss.Lock()
				wss.ws.Close()
				wss.Unlock()
				//s.notify <- true
				delete(m.subscribers, wss)
				//close(s.outbound)
			}
		}
	}
}
