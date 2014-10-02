package incus

import (
	"crypto/md5"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var DEBUG bool
var CLIENT_BROAD bool

const (
	writeWait = 5 * time.Second
	pongWait  = 1 * time.Second
)

type Server struct {
	ID     string
	Config *Configuration
	Store  *Storage

	Debug   bool
	timeout time.Duration
}

func CreateServer(conf *Configuration, store *Storage) *Server {
	hash := md5.New()
	io.WriteString(hash, time.Now().String())
	id := string(hash.Sum(nil))

	DEBUG = conf.GetBool("debug")
	CLIENT_BROAD = conf.GetBool("client_broadcasts")

	debug := conf.GetBool("debug")
	timeout := time.Duration(conf.GetInt("connection_timeout"))
	return &Server{ID: id, Config: conf, Store: store, Debug: debug, timeout: timeout}
}

func (this *Server) SocketListener(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	//if r.Header.Get("Origin") != "http://"+r.Host {
	//        http.Error(w, "Origin not allowed", 403)
	//        return
	// }

	ws, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshake", 400)
		return
	} else if err != nil {
		log.Println(err)
		return
	}

	defer func() {
		ws.Close()
		if this.Debug {
			log.Println("Web Socket Closed")
		}
	}()

	sock := newSocket(ws, nil, this, "")

	if this.Debug {
		log.Printf("Web Socket connected via %s\n", ws.RemoteAddr())
		//log.Printf("Web Socket connected via %s\n", ws.LocalAddr())
		log.Printf("Web Socket connected via %s\n", r.Header.Get("X-Forwarded-For"))
	}
	if err := sock.Authenticate(""); err != nil {
		if this.Debug {
			log.Printf("Web Socket Error: %s\n", err.Error())
		}
		return
	}

	/*	if this.Debug {
		log.Printf("Web Socket SID: %s, UID: %s\n", sock.SID, sock.UID)
		count, _ := this.Store.Count()
		log.Printf("Connected Clients: %d\n", count)
	}*/

	go sock.listenForMessages()
	go sock.listenForWrites()

	if this.timeout <= 0 { // if timeout is 0 then wait forever and return when socket is done.
		<-sock.done
		return
	}

	select {
	case <-time.After(this.timeout * time.Second):
		sock.Close()
		return
	case <-sock.done:
		return
	}
}

func (this *Server) LongPollListener(w http.ResponseWriter, r *http.Request) {
	defer func() {
		r.Body.Close()
		if this.Debug {
			log.Println("Longpoll Socket Closed")
		}
	}()

	sock := newSocket(nil, w, this, "")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store, no-cache, must-revalidate, post-check=0, pre-check=0")
	w.Header().Set("Connection", "keep-alive")
	//w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(200)

	if this.Debug {
		log.Printf("Long poll connected via %v \n", r.RemoteAddr)
		log.Printf("Long poll connected via %s\n", r.Header.Get("X-Forwarded-For"))
	}

	if err := sock.Authenticate(r.FormValue("user")); err != nil {
		if this.Debug {
			log.Printf("Long Poll Error: %s\n", err.Error())
		}
		return
	}

	/*	if this.Debug {
		log.Printf("Poll Socket SID: %s, UID: %s\n", sock.SID, sock.UID)
		count, _ := this.Store.Count()
		log.Printf("Connected Clients: %d\n", count)
	}*/

	page := r.FormValue("page")
	if page != "" {
		if sock.Page != "" {
			this.Store.UnsetPage(sock) //remove old page if it exists
		}

		sock.Page = page
		this.Store.SetPage(sock)
	}

	command := r.FormValue("command")
	if command != "" {
		var cmd = new(CommandMsg)
		json.Unmarshal([]byte(command), cmd)
		log.Printf("Longpoll cmd %v  command %v", cmd, command)
		go cmd.FromSocket(sock)
	}

	go sock.listenForWrites()

	select {
	case <-time.After(30 * time.Second):
		sock.Close()
		return
	case <-sock.done:
		return
	}
}

func (this *Server) RedisListener() {
	if !this.Config.GetBool("redis_enabled") {
		return
	}

	rec := make(chan []string, 10000)
	consumer, err := this.Store.redis.Subscribe(rec, this.Config.Get("redis_message_channel"))
	if err != nil {
		log.Fatal("Couldn't subscribe to redis channel")
	}
	defer consumer.Quit()

	if this.Debug {
		log.Println("LISENING FOR REDIS MESSAGE")
	}
	var ms []string
	for {
		ms = <-rec

		var cmd = new(CommandMsg)
		json.Unmarshal([]byte(ms[2]), cmd)
		go cmd.FromRedis(this)
	}
}

func (this *Server) AppListener(msg interface{}) {
	if this.Debug {
		log.Printf("LISENING FOR APP MESSAGE %v\n", msg)
	}

	var cmd = new(CommandMsg)
	//Command
	c := make(map[string]string)
	c["command"] = "message"
	cmd.Command = c

	// Message data
	d := make(map[string]interface{})
	d["mail"] = msg

	// Message
	m := make(map[string]interface{})
	m["data"] = d
	m["event"] = "NewMail"
	cmd.Message = m

	go cmd.FromApp(this)
}

func (this *Server) SendHeartbeats() {
	for {
		time.Sleep(20 * time.Second)
		clients := this.Store.Clients()

		for _, user := range clients {
			for _, sock := range user {
				if sock.isWebsocket() {
					if !sock.isClosed() {
						sock.ws.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(pongWait))
					}
				}
			}
		}
	}
}
