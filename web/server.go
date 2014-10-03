/*
	The web package contains all the code to provide SMTPD's web GUI
*/
package web

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gleez/smtpd/config"
	"github.com/gleez/smtpd/data"
	"github.com/gleez/smtpd/incus"
	"github.com/gleez/smtpd/log"
	"github.com/goods/httpbuf"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

type handler func(http.ResponseWriter, *http.Request, *Context) error

var webConfig config.WebConfig
var DataStore *data.DataStore
var Router *mux.Router
var listener net.Listener
var sessionStore sessions.Store

var shutdown bool
var Websocket *incus.Server

// Initialize sets up things for unit tests or the Start() method
func Initialize(cfg config.WebConfig, ds *data.DataStore) {
	webConfig = cfg
	setupWebSocket(cfg, ds)
	setupRoutes(cfg)

	// NewContext() will use this DataStore for the web handlers
	DataStore = ds
	sessionStore = sessions.NewCookieStore([]byte(cfg.CookieSecret))
}

// Initialize websocket from incus
func setupWebSocket(cfg config.WebConfig, ds *data.DataStore) {
	mymap := make(map[string]string)

	mymap["client_broadcasts"] = strconv.FormatBool(cfg.ClientBroadcasts)
	mymap["connection_timeout"] = strconv.Itoa(cfg.ConnTimeout)
	mymap["redis_enabled"] = strconv.FormatBool(cfg.RedisEnabled)
	mymap["debug"] = "true"

	conf := incus.InitConfig(mymap)
	store := incus.InitStore(&conf)
	Websocket = incus.CreateServer(&conf, store)

	log.LogInfo("Incus Websocket Init")

	go func() {
		for {
			select {
			case msg := <-ds.NotifyMailChan:
				go Websocket.AppListener(msg)
			}
		}
	}()

	go Websocket.RedisListener()
	go Websocket.SendHeartbeats()
}

func setupRoutes(cfg config.WebConfig) {
	log.LogInfo("Theme templates mapped to '%v'", cfg.TemplateDir)
	log.LogInfo("Theme static content mapped to '%v'", cfg.PublicDir)

	r := mux.NewRouter()

	// Static content
	r.PathPrefix("/public/").Handler(http.StripPrefix("/public/", http.FileServer(http.Dir(cfg.PublicDir))))

	// Register a couple of routes
	r.Path("/").Handler(handler(Home)).Name("Home").Methods("GET")
	r.Path("/status").Handler(handler(Status)).Name("Status").Methods("GET")

	// Mail
	r.Path("/mails").Handler(handler(MailList)).Name("Mails").Methods("GET")
	r.Path("/mails/{page:[0-9]+}").Handler(handler(MailList)).Name("MailList").Methods("GET")
	r.Path("/mail/{id:[0-9a-z]+}").Handler(handler(MailView)).Name("MailView").Methods("GET")
	r.Path("/mail/attachment/{id:[0-9a-z]+}/{[*.*]}").Handler(handler(MailAttachment)).Name("MailAttachment").Methods("GET")
	r.Path("/mail/delete/{id:[0-9a-z]+}").Handler(handler(MailDelete)).Name("MailDelete").Methods("GET")

	// Login
	r.Path("/login").Handler(handler(Login)).Methods("POST")
	r.Path("/login").Handler(handler(LoginForm)).Name("Login").Methods("GET")
	r.Path("/logout").Handler(handler(Logout)).Name("Logout").Methods("GET")
	r.Path("/register").Handler(handler(Register)).Methods("POST")
	r.Path("/register").Handler(handler(RegisterForm)).Name("Register").Methods("GET")

	// Add to greylist
	r.Path("/greylist/host/{id:[0-9a-z]+}").Handler(handler(MailView)).Name("GreyHostAdd").Methods("GET")
	r.Path("/greylist/mailfrom/{id:[0-9a-z]+}").Handler(handler(GreyMailFromAdd)).Name("GreyMailFromAdd").Methods("GET")
	r.Path("/greylist/tomail/{id:[0-9a-z]+}").Handler(handler(GreyMailFromAdd)).Name("GreyMailToAdd").Methods("GET")

	// Nginx Xclient auth
	r.Path("/auth-smtp").Handler(handler(NginxHTTPAuth)).Name("Nginx")
	r.Path("/ping").Handler(handler(Ping)).Name("Ping").Methods("GET")

	// Web-Socket & Fallback longpoll
	r.HandleFunc("/ws/", Websocket.SocketListener)
	r.HandleFunc("/lp", Websocket.LongPollListener)

	Router = r
	// Send all incoming requests to router.
	http.Handle("/", Router)
}

// Start() the web server
func Start() {
	addr := fmt.Sprintf("%v:%v", webConfig.Ip4address, webConfig.Ip4port)
	server := &http.Server{
		Addr:         addr,
		Handler:      nil,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	// We don't use ListenAndServe because it lacks a way to close the listener
	log.LogInfo("HTTP listening on TCP4 %v", addr)
	var err error
	listener, err = net.Listen("tcp", addr)
	if err != nil {
		log.LogError("HTTP failed to start TCP4 listener: %v", err)
		// TODO More graceful early-shutdown procedure
		panic(err)
	}

	err = server.Serve(listener)
	if shutdown {
		log.LogTrace("HTTP server shutting down on request")
	} else if err != nil {
		log.LogError("HTTP server failed: %v", err)
	}
}

func Stop() {
	log.LogTrace("HTTP shutdown requested")
	shutdown = true
	if listener != nil {
		listener.Close()
	} else {
		log.LogError("HTTP listener was nil during shutdown")
	}
}

// ServeHTTP builds the context and passes onto the real handler
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Create the context
	ctx, err := NewContext(req)
	if err != nil {
		log.LogError("Failed to create context: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer ctx.Close()

	// Run the handler, grab the error, and report it
	buf := new(httpbuf.Buffer)
	log.LogTrace("Web: %v %v %v %v", parseRemoteAddr(req), req.Proto, req.Method, req.RequestURI)
	err = h(buf, req, ctx)
	if err != nil {
		log.LogError("Error handling %v: %v", req.RequestURI, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Save the session
	if err = ctx.Session.Save(req, buf); err != nil {
		log.LogError("Failed to save session: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply the buffered response to the writer
	buf.Apply(w)
}

func message(title string, message string, ctype string) {
	//return RenderTemplate(w, r, "message.html", map[string]interface{}{"title": title, "message": template.HTML(message), "class": ctype})
}

func parseRemoteAddr(r *http.Request) string {
	if realip := r.Header.Get("X-Real-IP"); realip != "" {
		return realip
	}

	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		// X-Forwarded-For is potentially a list of addresses separated with ","
		parts := strings.Split(forwarded, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}

		// TODO: should return first non-local address
		return parts[0]
		//return forwarded
	}

	return r.RemoteAddr
}
