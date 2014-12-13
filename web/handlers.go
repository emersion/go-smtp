package web

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"io/ioutil"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/gleez/smtpd/config"
	"github.com/gleez/smtpd/data"
	"github.com/gleez/smtpd/log"

	"gopkg.in/mgo.v2/bson"
)

func MailAttachment(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	id := ctx.Vars["id"]
	log.LogTrace("Loading Attachment <%s> from Mongodb", id)

	//we need a user to sign to
	if ctx.User == nil {
		log.LogTrace("This page requires a login.")
		ctx.Session.AddFlash("This page requires a login.")
		return LoginForm(w, r, ctx)
	}

	m, err := ctx.Ds.LoadAttachment(id)
	if err != nil {
		return fmt.Errorf("ID provided is invalid: %v", err)
	}

	if len(m.Attachments) > 0 {
		at := m.Attachments[0]

		data, err := base64.StdEncoding.DecodeString(at.Body)
		if err != nil {
			return fmt.Errorf("Cannot decode attachment: %v", err)
		}

		reader := bytes.NewReader(data)
		w.Header().Set("Content-Type", at.ContentType)
		//w.Header().Set("Content-Disposition", "attachment; filename=\""+at.FileName+"\"")
		http.ServeContent(w, r, at.FileName, time.Now(), reader)
		return nil
	} else {
		http.NotFound(w, r)
		return
	}
}

func MailDelete(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	id := ctx.Vars["id"]
	log.LogTrace("Delete Mail <%s> from Mongodb", id)

	//we need a user to sign to
	if ctx.User == nil {
		log.LogTrace("This page requires a login.")
		ctx.Session.AddFlash("This page requires a login.")
		return LoginForm(w, r, ctx)
	}

	err = ctx.Ds.DeleteOne(id)
	if err == nil {
		log.LogTrace("Deleted mail id: %s", id)
		ctx.Session.AddFlash("Successfuly deleted mail id:" + id)
		http.Redirect(w, r, reverse("Mails"), http.StatusSeeOther)
		return nil
	} else {
		http.NotFound(w, r)
		return err
	}

}

func MailView(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	id := ctx.Vars["id"]
	log.LogTrace("Loading Mail <%s> from Mongodb", id)

	//we need a user to sign to
	if ctx.User == nil {
		log.LogTrace("This page requires a login.")
		ctx.Session.AddFlash("This page requires a login.")
		return LoginForm(w, r, ctx)
	}

	m, err := ctx.Ds.Load(id)
	if err == nil {
		ctx.Ds.Messages.Update(
			bson.M{"id": m.Id},
			bson.M{"$set": bson.M{"unread": false}},
		)
		return RenderTemplate("mailbox/_show.html", w, map[string]interface{}{
			"ctx":     ctx,
			"title":   "Mail",
			"message": m,
		})
	} else {
		http.NotFound(w, r)
		return
	}
}

func MailList(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	log.LogTrace("Loading Mails from Mongodb")

	page, _ := strconv.Atoi(ctx.Vars["page"])
	limit := 25

	//we need a user to sign to
	if ctx.User == nil {
		log.LogTrace("This page requires a login.")
		ctx.Session.AddFlash("This page requires a login.")
		return LoginForm(w, r, ctx)
	}

	t, err := ctx.Ds.Total()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	p := NewPagination(t, limit, page, "/mails")
	if page > p.Pages() {
		http.NotFound(w, r)
		return
	}

	messages, err := ctx.Ds.List(p.Offset(), p.Limit())
	if err == nil {
		return RenderTemplate("mailbox/_list.html", w, map[string]interface{}{
			"ctx":        ctx,
			"title":      "Mails",
			"messages":   messages,
			"end":        p.Offset() + p.Limit(),
			"pagination": p,
		})
	} else {
		http.NotFound(w, r)
		return
	}
}

func Home(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	greeting, err := ioutil.ReadFile(config.GetWebConfig().GreetingFile)
	if err != nil {
		fmt.Errorf("Failed to load greeting: %v", err)
	}

	return RenderTemplate("root/index.html", w, map[string]interface{}{
		"ctx":      ctx,
		"greeting": template.HTML(string(greeting)),
	})
}

func LoginForm(w http.ResponseWriter, req *http.Request, ctx *Context) (err error) {
	t := NewCSRF("login", ctx.Session.ID, 20)
	return RenderTemplate("common/login.html", w, map[string]interface{}{
		"ctx":   ctx,
		"Token": t.Generate(),
	})
}

func Login(w http.ResponseWriter, req *http.Request, ctx *Context) error {
	l := &data.LoginForm{
		Username: req.FormValue("username"),
		Password: req.FormValue("password"),
	}

	if l.Validate() {
		u, err := ctx.Ds.Login(l.Username, l.Password)

		if err == nil {
			//store the user id in the values and redirect to index
			log.LogTrace("Login successful for session <%v>", u.Id)
			ctx.Ds.Users.Update(
				bson.M{"_id": u.Id},
				bson.M{"$set": bson.M{"lastlogintime": time.Now(), "lastloginip": ctx.ClientIp}, "$inc": bson.M{"logincount": 1}},
			)

			if u.IsActive {
				ctx.Session.Values["user"] = u.Id.Hex()
				http.Redirect(w, req, reverse("Mails"), http.StatusSeeOther)
				return nil
			} else {
				log.LogTrace("The user is not activated")
				ctx.Session.AddFlash("Username is not activated")
				return LoginForm(w, req, ctx)
			}
		} else {
			log.LogTrace("Invalid Username/Password")
			ctx.Session.AddFlash("Invalid Username/Password")
			return LoginForm(w, req, ctx)
		}
	} else {
		ctx.Session.AddFlash("Please fill all fields!")
		return LoginForm(w, req, ctx)
	}

	return fmt.Errorf("Failed to login!")
}

func Logout(w http.ResponseWriter, req *http.Request, ctx *Context) error {
	delete(ctx.Session.Values, "user")
	http.Redirect(w, req, reverse("Home"), http.StatusSeeOther)
	return nil
}

func RegisterForm(w http.ResponseWriter, req *http.Request, ctx *Context) (err error) {
	if ctx.User != nil {
		ctx.Session.AddFlash("Already logged in")
		http.Redirect(w, req, reverse("Mails"), http.StatusSeeOther)
	}

	return RenderTemplate("common/signup.html", w, map[string]interface{}{
		"ctx": ctx,
	})
}

func Register(w http.ResponseWriter, req *http.Request, ctx *Context) error {
	if ctx.User != nil {
		ctx.Session.AddFlash("Already logged in")
		http.Redirect(w, req, reverse("Mails"), http.StatusSeeOther)
	}

	r := &data.LoginForm{
		Username: req.FormValue("username"),
		Password: req.FormValue("password"),
	}

	if r.Validate() {
		result := &data.User{}
		err := ctx.Ds.Users.Find(bson.M{"username": r.Username}).One(&result)
		if err == nil {
			ctx.Session.AddFlash("User already exists!")
			return RegisterForm(w, req, ctx)
		}

		u := &data.User{
			Id:          bson.NewObjectId(),
			Firstname:   req.FormValue("firstname"),
			Lastname:    req.FormValue("lastname"),
			Email:       req.FormValue("email"),
			Username:    r.Username,
			IsActive:    false,
			JoinedAt:    time.Now(),
			LastLoginIp: ctx.ClientIp,
		}
		u.SetPassword(r.Password)

		if err := ctx.Ds.Users.Insert(u); err != nil {
			ctx.Session.AddFlash("Problem registering user.")
			return RegisterForm(w, req, ctx)
		}

		if u.IsActive {
			//store the user id in the values and redirect to index
			ctx.Session.Values["user"] = u.Id.Hex()
			ctx.Session.AddFlash("Registration successful")
			http.Redirect(w, req, reverse("Mails"), http.StatusSeeOther)
			return nil
		} else {
			log.LogTrace("Registration successful")
			ctx.Session.AddFlash("Registration successful")
			return LoginForm(w, req, ctx)
		}
	} else {
		ctx.Session.AddFlash("Please fill all fields!")
		return RegisterForm(w, req, ctx)
	}

	return fmt.Errorf("Failed to register!")
}

func GreyMailFromAdd(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	id := ctx.Vars["id"]
	log.LogTrace("Greylist add mail %s", id)

	//we need a user to sign to
	if ctx.User == nil {
		log.LogWarn("Please login to add to grey list!")
		http.NotFound(w, r)
		return
	}

	// we need a user to be admin
	if ctx.User.IsSuperuser == false {
		http.NotFound(w, r)
		return
	}

	// we need to load email
	m, err := ctx.Ds.Load(id)
	if err != nil {
		log.LogTrace("Greylist mail Id not found %s", id)
		http.NotFound(w, r)
		return
	}

	e := fmt.Sprintf("%s@%s", m.From.Mailbox, m.From.Domain)
	if to, _ := ctx.Ds.IsGreyMail(e, "from"); to == 0 {
		log.LogTrace("Greylist inserting mail %s", e)
		gm := data.GreyMail{
			Id:        bson.NewObjectId(),
			CreatedBy: ctx.User.Id.Hex(),
			CreatedAt: time.Now(),
			IsActive:  true,
			Email:     e,
			Local:     m.From.Mailbox,
			Domain:    m.From.Domain,
			Type:      "from",
		}

		if err = ctx.Ds.Emails.Insert(gm); err != nil {
			log.LogError("Error inserting grey list: %s", err)
			http.NotFound(w, r)
			return
		}

		return
	}

	http.NotFound(w, r)
	return
}

func GreyMailToAdd(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	id := ctx.Vars["id"]
	log.LogTrace("Greylist add mail %s", id)

	//we need a user to sign to
	if ctx.User == nil {
		log.LogWarn("Please login to add to grey list!")
		http.NotFound(w, r)
		return
	}

	// we need a user to be admin
	if ctx.User.IsSuperuser == false {
		http.NotFound(w, r)
		return
	}

	// we need to load email
	m, err := ctx.Ds.Load(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	e := fmt.Sprintf("%s@%s", m.From.Mailbox, m.From.Domain)
	if to, _ := ctx.Ds.IsGreyMail(e, "to"); to == 0 {
		log.LogTrace("Greylist inserting mail %s", e)
		gm := data.GreyMail{
			Id:        bson.NewObjectId(),
			CreatedBy: ctx.User.Id.Hex(),
			CreatedAt: time.Now(),
			IsActive:  true,
			Email:     e,
			Local:     m.From.Mailbox,
			Domain:    m.From.Domain,
			Type:      "to",
		}

		if err = ctx.Ds.Emails.Insert(gm); err != nil {
			log.LogError("Error inserting grey list: %s", err)
			http.NotFound(w, r)
			return
		}

		return
	}

	http.NotFound(w, r)
	return
}

func Status(w http.ResponseWriter, r *http.Request, ctx *Context) (err error) {
	//we need a user to sign to
	if ctx.User == nil {
		log.LogTrace("This page requires a login.")
		ctx.Session.AddFlash("This page requires a login.")
		return LoginForm(w, r, ctx)
	}

	updateSystemStatus()

	return RenderTemplate("root/status.html", w, map[string]interface{}{
		"ctx":       ctx,
		"SysStatus": sysStatus,
	})
}

func Ping(w http.ResponseWriter, r *http.Request, ctx *Context) error {
	log.LogTrace("Ping successful")
	fmt.Fprint(w, "OK")
	return nil
}

// If running Nginx as a proxy, give Nginx the IP address and port for the SMTP server
// Primary use of Nginx is to terminate TLS so that Go doesn't need to deal with it.
// This could perform auth and load balancing too
// See http://wiki.nginx.org/MailCoreModule
func NginxHTTPAuth(w http.ResponseWriter, r *http.Request, ctx *Context) error {
	log.LogTrace("Nginx Auth Client: %s", parseRemoteAddr(r))

	cfg := config.GetSmtpConfig()

	w.Header().Add("Auth-Status", "OK")
	w.Header().Add("Auth-Server", cfg.Ip4address.String())
	w.Header().Add("Auth-Port", strconv.Itoa(cfg.Ip4port))
	fmt.Fprint(w, "")
	return nil
}

var (
	startTime = time.Now()
)

var sysStatus struct {
	Uptime       string
	NumGoroutine int

	// General statistics.
	MemAllocated string // bytes allocated and still in use
	MemTotal     string // bytes allocated (even if freed)
	MemSys       string // bytes obtained from system (sum of XxxSys below)
	Lookups      uint64 // number of pointer lookups
	MemMallocs   uint64 // number of mallocs
	MemFrees     uint64 // number of frees

	// Main allocation heap statistics.
	HeapAlloc    string // bytes allocated and still in use
	HeapSys      string // bytes obtained from system
	HeapIdle     string // bytes in idle spans
	HeapInuse    string // bytes in non-idle span
	HeapReleased string // bytes released to the OS
	HeapObjects  uint64 // total number of allocated objects

	// Low-level fixed-size structure allocator statistics.
	//	Inuse is bytes used now.
	//	Sys is bytes obtained from system.
	StackInuse  string // bootstrap stacks
	StackSys    string
	MSpanInuse  string // mspan structures
	MSpanSys    string
	MCacheInuse string // mcache structures
	MCacheSys   string
	BuckHashSys string // profiling bucket hash table
	GCSys       string // GC metadata
	OtherSys    string // other system allocations

	// Garbage collector statistics.
	NextGC       string // next run in HeapAlloc time (bytes)
	LastGC       string // last run in absolute time (ns)
	PauseTotalNs string
	PauseNs      string // circular buffer of recent GC pause times, most recent at [(NumGC+255)%256]
	NumGC        uint32
}

func updateSystemStatus() {
	//time.Since(startTime) / time.Second
	sysStatus.Uptime = time.Since(startTime).String()

	m := new(runtime.MemStats)
	runtime.ReadMemStats(m)
	sysStatus.NumGoroutine = runtime.NumGoroutine()

	sysStatus.MemAllocated = FileSize(int64(m.Alloc))
	sysStatus.MemTotal = FileSize(int64(m.TotalAlloc))
	sysStatus.MemSys = FileSize(int64(m.Sys))
	sysStatus.Lookups = m.Lookups
	sysStatus.MemMallocs = m.Mallocs
	sysStatus.MemFrees = m.Frees

	sysStatus.HeapAlloc = FileSize(int64(m.HeapAlloc))
	sysStatus.HeapSys = FileSize(int64(m.HeapSys))
	sysStatus.HeapIdle = FileSize(int64(m.HeapIdle))
	sysStatus.HeapInuse = FileSize(int64(m.HeapInuse))
	sysStatus.HeapReleased = FileSize(int64(m.HeapReleased))
	sysStatus.HeapObjects = m.HeapObjects

	sysStatus.StackInuse = FileSize(int64(m.StackInuse))
	sysStatus.StackSys = FileSize(int64(m.StackSys))
	sysStatus.MSpanInuse = FileSize(int64(m.MSpanInuse))
	sysStatus.MSpanSys = FileSize(int64(m.MSpanSys))
	sysStatus.MCacheInuse = FileSize(int64(m.MCacheInuse))
	sysStatus.MCacheSys = FileSize(int64(m.MCacheSys))
	sysStatus.BuckHashSys = FileSize(int64(m.BuckHashSys))
	sysStatus.GCSys = FileSize(int64(m.GCSys))
	sysStatus.OtherSys = FileSize(int64(m.OtherSys))

	sysStatus.NextGC = FileSize(int64(m.NextGC))
	sysStatus.LastGC = fmt.Sprintf("%.1fs", float64(time.Now().UnixNano()-int64(m.LastGC))/1000/1000/1000)
	sysStatus.PauseTotalNs = fmt.Sprintf("%.1fs", float64(m.PauseTotalNs)/1000/1000/1000)
	sysStatus.PauseNs = fmt.Sprintf("%.3fs", float64(m.PauseNs[(m.NumGC+255)%256])/1000/1000/1000)
	sysStatus.NumGC = m.NumGC
}

func logn(n, b float64) float64 {
	return math.Log(n) / math.Log(b)
}

func humanateBytes(s uint64, base float64, sizes []string) string {
	if s < 10 {
		return fmt.Sprintf("%dB", s)
	}
	e := math.Floor(logn(float64(s), base))
	suffix := sizes[int(e)]
	val := float64(s) / math.Pow(base, math.Floor(e))
	f := "%.0f"
	if val < 10 {
		f = "%.1f"
	}

	return fmt.Sprintf(f+"%s", val, suffix)
}

// FileSize calculates the file size and generate user-friendly string.
func FileSize(s int64) string {
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	return humanateBytes(uint64(s), 1024, sizes)
}
