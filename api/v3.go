package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/bobrnor/MailHog-Server/config"
	"github.com/bobrnor/MailHog-Server/monkey"
	"github.com/bobrnor/MailHog-Server/websockets"
	"github.com/bobrnor/storage"
	"github.com/gorilla/mux"
	"github.com/gorilla/pat"
	"github.com/ian-kent/go-log/log"
	"github.com/mailhog/data"
)

// APIv3 implements version 3 of the MailHog API
//
// It is currently experimental and may change in future releases.
// Use APIv1 for guaranteed compatibility.
type APIv3 struct {
	config      *config.Config
	messageChan chan *data.Message
	wsHub       *websockets.Hub
}

func createAPIv3(conf *config.Config, r *pat.Router) *APIv3 {
	log.Println("Creating API v3 with WebPath: " + conf.WebPath)
	apiv3 := &APIv3{
		config:      conf,
		messageChan: make(chan *data.Message),
		wsHub:       websockets.NewHub(),
	}

	r.Path(conf.WebPath + "/api/v3/{namespace}/messages").Methods("GET").HandlerFunc(apiv3.messages)
	r.Path(conf.WebPath + "/api/v3/{namespace}/messages").Methods("OPTIONS").HandlerFunc(apiv3.defaultOptions)

	r.Path(conf.WebPath + "/api/v3/{namespace}/search").Methods("GET").HandlerFunc(apiv3.search)
	r.Path(conf.WebPath + "/api/v3/{namespace}/search").Methods("OPTIONS").HandlerFunc(apiv3.defaultOptions)

	r.Path(conf.WebPath + "/api/v3/jim").Methods("GET").HandlerFunc(apiv3.jim)
	r.Path(conf.WebPath + "/api/v3/jim").Methods("POST").HandlerFunc(apiv3.createJim)
	r.Path(conf.WebPath + "/api/v3/jim").Methods("PUT").HandlerFunc(apiv3.updateJim)
	r.Path(conf.WebPath + "/api/v3/jim").Methods("DELETE").HandlerFunc(apiv3.deleteJim)
	r.Path(conf.WebPath + "/api/v3/jim").Methods("OPTIONS").HandlerFunc(apiv3.defaultOptions)

	r.Path(conf.WebPath + "/api/v3/outgoing-smtp").Methods("GET").HandlerFunc(apiv3.listOutgoingSMTP)
	r.Path(conf.WebPath + "/api/v3/outgoing-smtp").Methods("OPTIONS").HandlerFunc(apiv3.defaultOptions)

	r.Path(conf.WebPath + "/api/v3/{namespace}/websocket").Methods("GET").HandlerFunc(apiv3.websocket)

	go func() {
		for {
			select {
			case msg := <-apiv3.messageChan:
				log.Println("Got message in APIv3 websocket channel")
				apiv3.broadcast(msg)
			}
		}
	}()

	return apiv3
}

func (apiv3 *APIv3) defaultOptions(w http.ResponseWriter, req *http.Request) {
	if len(apiv3.config.CORSOrigin) > 0 {
		w.Header().Add("Access-Control-Allow-Origin", apiv3.config.CORSOrigin)
		w.Header().Add("Access-Control-Allow-Methods", "OPTIONS,GET,PUT,POST,DELETE")
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
	}
}

type messagesResultV3 struct {
	Total int            `json:"total"`
	Count int            `json:"count"`
	Start int            `json:"start"`
	Items []data.Message `json:"items"`
}

func (apiv3 *APIv3) getStartLimit(w http.ResponseWriter, req *http.Request) (start, limit int) {
	start = 0
	limit = 50

	s := req.URL.Query().Get("start")
	if n, e := strconv.ParseInt(s, 10, 64); e == nil && n > 0 {
		start = int(n)
	}

	l := req.URL.Query().Get("limit")
	if n, e := strconv.ParseInt(l, 10, 64); e == nil && n > 0 {
		if n > 250 {
			n = 250
		}
		limit = int(n)
	}

	return
}

func (apiv3 *APIv3) messages(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/{namespace}/messages")

	apiv3.defaultOptions(w, req)

	start, limit := apiv3.getStartLimit(w, req)

	ns := mux.Vars(req)["namespace"]

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var res messagesResultV3

	messages, err := apiv3.config.Storage.(storage.StorageWithNamespace).ListWithNamespace(ns, start, limit)
	if err != nil {
		panic(err)
	}

	res.Count = len([]data.Message(*messages))
	res.Start = start
	res.Items = []data.Message(*messages)
	res.Total = apiv3.config.Storage.Count()

	bytes, _ := json.Marshal(res)
	w.Header().Add("Content-Type", "text/json")
	w.Write(bytes)
}

func (apiv3 *APIv3) search(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/{namespace}/search")

	apiv3.defaultOptions(w, req)

	start, limit := apiv3.getStartLimit(w, req)

	kind := req.URL.Query().Get("kind")
	if kind != "from" && kind != "to" && kind != "containing" {
		w.WriteHeader(400)
		return
	}

	query := req.URL.Query().Get("query")
	if len(query) == 0 {
		w.WriteHeader(400)
		return
	}

	ns := mux.Vars(req)["namespace"]

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var res messagesResultV3

	messages, total, _ := apiv3.config.Storage.(storage.StorageWithNamespace).SearchWithNamespace(ns, kind, query, start, limit)

	res.Count = len([]data.Message(*messages))
	res.Start = start
	res.Items = []data.Message(*messages)
	res.Total = total

	b, _ := json.Marshal(res)
	w.Header().Add("Content-Type", "application/json")
	w.Write(b)
}

func (apiv3 *APIv3) jim(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/jim")

	apiv3.defaultOptions(w, req)

	if apiv3.config.Monkey == nil {
		w.WriteHeader(404)
		return
	}

	b, _ := json.Marshal(apiv3.config.Monkey)
	w.Header().Add("Content-Type", "application/json")
	w.Write(b)
}

func (apiv3 *APIv3) deleteJim(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] DELETE /api/v3/jim")

	apiv3.defaultOptions(w, req)

	if apiv3.config.Monkey == nil {
		w.WriteHeader(404)
		return
	}

	apiv3.config.Monkey = nil
}

func (apiv3 *APIv3) createJim(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] POST /api/v3/jim")

	apiv3.defaultOptions(w, req)

	if apiv3.config.Monkey != nil {
		w.WriteHeader(400)
		return
	}

	apiv3.config.Monkey = config.Jim

	// Try, but ignore errors
	// Could be better (e.g., ok if no json, error if badly formed json)
	// but this works for now
	apiv3.newJimFromBody(w, req)

	w.WriteHeader(201)
}

func (apiv3 *APIv3) newJimFromBody(w http.ResponseWriter, req *http.Request) error {
	var jim monkey.Jim

	dec := json.NewDecoder(req.Body)
	err := dec.Decode(&jim)

	if err != nil {
		return err
	}

	jim.ConfigureFrom(config.Jim)

	config.Jim = &jim
	apiv3.config.Monkey = &jim

	return nil
}

func (apiv3 *APIv3) updateJim(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] PUT /api/v3/jim")

	apiv3.defaultOptions(w, req)

	if apiv3.config.Monkey == nil {
		w.WriteHeader(404)
		return
	}

	err := apiv3.newJimFromBody(w, req)
	if err != nil {
		w.WriteHeader(400)
	}
}

func (apiv3 *APIv3) listOutgoingSMTP(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/outgoing-smtp")

	apiv3.defaultOptions(w, req)

	b, _ := json.Marshal(apiv3.config.OutgoingSMTP)
	w.Header().Add("Content-Type", "application/json")
	w.Write(b)
}

func (apiv3 *APIv3) websocket(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/{namespace}/websocket")

	ns := mux.Vars(req)["namespace"]

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	apiv3.wsHub.ServeWithNamespace(ns, w, req)
}

func (apiv3 *APIv3) broadcast(msg *data.Message) {
	log.Println("[APIv3] BROADCAST /api/v3/websocket")

	apiv3.wsHub.Broadcast(msg)
}
