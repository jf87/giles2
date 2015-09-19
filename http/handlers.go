// Package http implements an HTTP interface to the Archiver API at
// http://godoc.org/github.com/gtfierro/2giles/archiver
//
// An example of a valid sMAP object is
//    {
//      "/sensor0" : {
//        "Metadata" : {
//          "SourceName" : "Test Source",
//            "Location" : { "City" : "Berkeley" }
//        },
//          "Properties": {
//            "Timezone": "America/Los_Angeles",
//            "UnitofMeasure": "Watt",
//            "UnitofTime": "ms",
//            "StreamType": "numeric",
//            "ReadingType": "double"
//          },
//          "Readings" : [[1351043674000, 0], [1351043675000, 1]],
//          "uuid" : "d24325e6-1d7d-11e2-ad69-a7c2fa8dba61"
//      }
//    }
package http

import (
	"encoding/json"
	"github.com/gtfierro/giles2/archiver"
	"github.com/julienschmidt/httprouter"
	"github.com/op/go-logging"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
)

// logger
var log *logging.Logger

// set up logging facilities
func init() {
	log = logging.MustGetLogger("archiver")
	var format = "%{color}%{level} %{time:Jan 02 15:04:05} %{shortfile}%{color:reset} ▶ %{message}"
	var logBackend = logging.NewLogBackend(os.Stderr, "", 0)
	logBackendLeveled := logging.AddModuleLevel(logBackend)
	logging.SetBackend(logBackendLeveled)
	logging.SetFormatter(logging.MustStringFormatter(format))
}

type HTTPHandler struct {
	a *archiver.Archiver
}

func Handle(a *archiver.Archiver, port int) {
	r := httprouter.New()
	h := &HTTPHandler{a}
	r.POST("/add/:key", h.handleAdd)
	r.POST("/api/query/:key", h.handleSingleQuery)
	r.POST("/api/query", h.handleSingleQuery)
	address, err := net.ResolveTCPAddr("tcp4", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		log.Fatal("Error resolving address %v: %v", "0.0.0.0:"+strconv.Itoa(port), err)
	}
	http.Handle("/", r)
	log.Notice("Starting HTTP on %v", address.String())

	srv := &http.Server{
		Addr: address.String(),
	}
	srv.ListenAndServe()
}

func (h *HTTPHandler) handleAdd(rw http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var (
		ephkey   archiver.EphemeralKey
		messages archiver.TieredSmapMessage
		err      error
		msgSync  sync.WaitGroup
	)
	copy(ephkey[:], ps.ByName("key"))

	if messages, err = handleJSON(req.Body); err != nil {
		log.Error("Error handling JSON: %v", err)
		rw.WriteHeader(500)
		rw.Write([]byte(err.Error()))
		req.Body.Close()
		return
	}

	messages.CollapseToTimeseries()
	msgSync.Add(len(messages))
	for _, msg := range messages {
		go func(msg *archiver.SmapMessage) {
			if addErr := h.a.AddData(msg, ephkey); addErr != nil {
				err = addErr
			}
			msgSync.Done()
		}(msg)
	}

	msgSync.Wait()
	rw.WriteHeader(200)
}

func (h *HTTPHandler) handleSingleQuery(rw http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var (
		ephkey archiver.EphemeralKey
		err    error
	)
	copy(ephkey[:], ps.ByName("key"))

	if req.ContentLength > 1024 {
		log.Error("HUGE query string with length %v. Aborting!", req.ContentLength)
		rw.WriteHeader(500)
		rw.Write([]byte("Your query is too big"))
		req.Body.Close()
		return
	}

	querybuffer := make([]byte, req.ContentLength)
	_, err = req.Body.Read(querybuffer)
	res, err := h.a.HandleQuery(string(querybuffer), ephkey)
	if err != nil {
		log.Error("Error evaluating query: %v", err)
		rw.WriteHeader(500)
		rw.Write([]byte(err.Error()))
		return
	}
	writer := json.NewEncoder(rw)
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	err = writer.Encode(res)
	if err != nil {
		log.Error("Error converting query results to JSON: %v", err)
	}
}

func handleJSON(r io.Reader) (decoded archiver.TieredSmapMessage, err error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	err = decoder.Decode(&decoded)
	for path, msg := range decoded {
		msg.Path = path
	}
	return
}
