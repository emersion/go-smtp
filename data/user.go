package data

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"gopkg.in/mgo.v2/bson"
)

type User struct {
	Id            bson.ObjectId `bson:"_id"`
	Firstname     string
	Lastname      string
	Email         string
	Username      string
	Password      string
	Avatar        string
	Website       string
	Location      string
	Tagline       string
	Bio           string
	JoinedAt      time.Time
	IsSuperuser   bool
	IsActive      bool
	ValidateCode  string
	ResetCode     string
	LastLoginTime time.Time
	LastLoginIp   string
	LoginCount    int64
}

type LoginForm struct {
	Username string
	Password string
	Token    string

	Errors map[string]string
}

func (f *LoginForm) Validate() bool {
	f.Errors = make(map[string]string)

	if strings.TrimSpace(f.Username) == "" {
		f.Errors["Username"] = "Please enter a valid username"
	}

	if strings.TrimSpace(f.Password) == "" {
		f.Errors["Password"] = "Please enter a password"
	}

	/*	re := regexp.MustCompile(".+@.+\\..+")
		matched := re.Match([]byte(f.Email))
		if matched == false {
			f.Errors["Email"] = "Please enter a valid email address"
		}*/

	return len(f.Errors) == 0
}

func Encrypt_Password(password string, salt []byte) string {
	if salt == nil {
		m := md5.New()
		m.Write([]byte(time.Now().String()))
		s := hex.EncodeToString(m.Sum(nil))
		salt = []byte(s[2:10])
	}
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(password))
	//s := fmt.Sprintf("%x", (mac.Sum(salt)))
	s := hex.EncodeToString(mac.Sum(nil))

	hasher := sha1.New()
	hasher.Write([]byte(s))

	//result := fmt.Sprintf("%x", (hasher.Sum(nil)))
	result := hex.EncodeToString(hasher.Sum(nil))

	p := string(salt) + result

	return p
}

func Validate_Password(hashed string, input_password string) bool {
	salt := hashed[0:8]
	if hashed == Encrypt_Password(input_password, []byte(salt)) {
		return true
	} else {
		return false
	}
	return false
}

//SetPassword takes a plaintext password and hashes it with bcrypt and sets the
//password field to the hash.
func (u *User) SetPassword(password string) {
	u.Password = Encrypt_Password(password, nil)
}
