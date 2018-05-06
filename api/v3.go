package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/bobrnor/MailHog-Server/config"
	"github.com/bobrnor/MailHog-Server/websockets"
	"github.com/bobrnor/storage"
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

	r.Path(conf.WebPath + "/api/v3/namespaces").Methods("GET").HandlerFunc(apiv3.namespaces)

	r.Path(conf.WebPath + "/api/v3/{namespace}/messages").Methods("GET").HandlerFunc(apiv3.messages)
	r.Path(conf.WebPath + "/api/v3/{namespace}/messages").Methods("OPTIONS").HandlerFunc(apiv3.defaultOptions)
	r.Path(conf.WebPath + "/api/v3/{namespace}/messages").Methods("DELETE").HandlerFunc(apiv3.delete_all)

	r.Path(conf.WebPath + "/api/v3/{namespace}/messages/{id}").Methods("DELETE").HandlerFunc(apiv3.delete_one)

	r.Path(conf.WebPath + "/api/v3/{namespace}/search").Methods("GET").HandlerFunc(apiv3.search)
	r.Path(conf.WebPath + "/api/v3/{namespace}/search").Methods("OPTIONS").HandlerFunc(apiv3.defaultOptions)

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

func (apiv3 *APIv3) namespaces(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/namespaces")

	apiv3.defaultOptions(w, req)

	s, ok := apiv3.config.Storage.(storage.StorageWithNamespace)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	res, err := s.ListNamespaces()
	if err != nil {
		panic(err)
	}

	bytes, _ := json.Marshal(res)
	w.Header().Add("Content-Type", "text/json")
	w.Write(bytes)
}

func (apiv3 *APIv3) messages(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/{namespace}/messages")

	apiv3.defaultOptions(w, req)

	start, limit := apiv3.getStartLimit(w, req)

	ns := req.URL.Query().Get(":namespace")

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var res messagesResultV3

	s, ok := apiv3.config.Storage.(storage.StorageWithNamespace)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	messages, err := s.ListWithNamespace(ns, start, limit)
	if err != nil {
		panic(err)
	}

	res.Count = len([]data.Message(*messages))
	res.Start = start
	res.Items = []data.Message(*messages)
	res.Total = s.CountWithNamespace(ns)

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

	ns := req.URL.Query().Get(":namespace")

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var res messagesResultV3

	s, ok := apiv3.config.Storage.(storage.StorageWithNamespace)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	messages, total, _ := s.SearchWithNamespace(ns, kind, query, start, limit)

	res.Count = len([]data.Message(*messages))
	res.Start = start
	res.Items = []data.Message(*messages)
	res.Total = total

	b, _ := json.Marshal(res)
	w.Header().Add("Content-Type", "application/json")
	w.Write(b)
}

func (apiv3 *APIv3) delete_all(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] POST /api/v3/{namespace}/messages")

	apiv3.defaultOptions(w, req)

	w.Header().Add("Content-Type", "text/json")

	ns := req.URL.Query().Get(":namespace")

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s, ok := apiv3.config.Storage.(storage.StorageWithNamespace)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err := s.DeleteAllWithNamespace(ns)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(200)
}

func (apiv3 *APIv3) delete_one(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get(":id")

	log.Printf("[APIv3] POST /api/v3/{namespace}/messages/%s/delete\n", id)

	apiv3.defaultOptions(w, req)

	w.Header().Add("Content-Type", "text/json")

	ns := req.URL.Query().Get(":namespace")

	if len(ns) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s, ok := apiv3.config.Storage.(storage.StorageWithNamespace)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err := s.DeleteOneWithNamespace(ns, id)
	if err != nil {
		log.Println(err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(200)
}

func (apiv3 *APIv3) websocket(w http.ResponseWriter, req *http.Request) {
	log.Println("[APIv3] GET /api/v3/{namespace}/websocket")

	ns := req.URL.Query().Get(":namespace")

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
