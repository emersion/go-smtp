package web

import (
	"net/http"
	"strings"

	"github.com/gleez/smtpd/data"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"labix.org/v2/mgo/bson"
)

type Context struct {
	Vars      map[string]string
	Session   *sessions.Session
	DataStore *data.DataStore
	IsJson    bool
	User      *data.User
	ClientIp  string
	Ds        *data.MongoDB
}

func (c *Context) Close() {
	// Do nothing
}

// headerMatch returns true if the request header specified by name contains
// the specified value.  Case is ignored.
func headerMatch(req *http.Request, name string, value string) bool {
	name = http.CanonicalHeaderKey(name)
	value = strings.ToLower(value)

	if header := req.Header[name]; header != nil {
		for _, hv := range header {
			if value == strings.ToLower(hv) {
				return true
			}
		}
	}

	return false
}

func NewContext(req *http.Request) (*Context, error) {
	vars := mux.Vars(req)
	sess, err := sessionStore.Get(req, "gsmtpd")
	ctx := &Context{
		Vars:      vars,
		Session:   sess,
		DataStore: DataStore,
		ClientIp:  parseRemoteAddr(req),
		IsJson:    headerMatch(req, "Accept", "application/json"),
		Ds:        DataStore.Storage.(*data.MongoDB),
	}

	if err != nil {
		return ctx, err
	}

	//try to fill in the user from the session
	if user, ok := sess.Values["user"].(string); ok {
		if bson.IsObjectIdHex(user) {
			uid := bson.ObjectIdHex(user)
			err := ctx.Ds.Users.Find(bson.M{"_id": uid}).One(&ctx.User)

			if err != nil {
				ctx.User = nil
				return ctx, nil
			}
		} else {
			ctx.User = nil
		}
	}

	return ctx, err
}
