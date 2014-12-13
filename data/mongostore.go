package data

import (
	"fmt"

	"github.com/gleez/smtpd/config"
	"github.com/gleez/smtpd/log"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type MongoDB struct {
	Config   config.DataStoreConfig
	Session  *mgo.Session
	Messages *mgo.Collection
	Users    *mgo.Collection
	Hosts    *mgo.Collection
	Emails   *mgo.Collection
}

var (
	mgoSession *mgo.Session
)

func getSession(c config.DataStoreConfig) *mgo.Session {
	if mgoSession == nil {
		var err error
		mgoSession, err = mgo.Dial(c.MongoUri)
		if err != nil {
			log.LogError("Session Error connecting to MongoDB: %s", err)
			return nil
		}
	}
	return mgoSession.Clone()
}

func CreateMongoDB(c config.DataStoreConfig) *MongoDB {
	log.LogTrace("Connecting to MongoDB: %s\n", c.MongoUri)

	session, err := mgo.Dial(c.MongoUri)
	if err != nil {
		log.LogError("Error connecting to MongoDB: %s", err)
		return nil
	}

	return &MongoDB{
		Config:   c,
		Session:  session,
		Messages: session.DB(c.MongoDb).C(c.MongoColl),
		Users:    session.DB(c.MongoDb).C("Users"),
		Hosts:    session.DB(c.MongoDb).C("GreyHosts"),
		Emails:   session.DB(c.MongoDb).C("GreyMails"),
	}
}

func (mongo *MongoDB) Close() {
	mongo.Session.Close()
}

func (mongo *MongoDB) Store(m *Message) (string, error) {
	err := mongo.Messages.Insert(m)
	if err != nil {
		log.LogError("Error inserting message: %s", err)
		return "", err
	}
	return m.Id, nil
}

func (mongo *MongoDB) List(start int, limit int) (*Messages, error) {
	messages := &Messages{}
	err := mongo.Messages.Find(bson.M{}).Sort("-_id").Skip(start).Limit(limit).Select(bson.M{
		"id":          1,
		"from":        1,
		"to":          1,
		"attachments": 1,
		"created":     1,
		"ip":          1,
		"subject":     1,
		"starred":     1,
		"unread":      1,
	}).All(messages)
	if err != nil {
		log.LogError("Error loading messages: %s", err)
		return nil, err
	}
	return messages, nil
}

func (mongo *MongoDB) DeleteOne(id string) error {
	_, err := mongo.Messages.RemoveAll(bson.M{"id": id})
	return err
}

func (mongo *MongoDB) DeleteAll() error {
	_, err := mongo.Messages.RemoveAll(bson.M{})
	return err
}

func (mongo *MongoDB) Load(id string) (*Message, error) {
	result := &Message{}
	err := mongo.Messages.Find(bson.M{"id": id}).One(&result)
	if err != nil {
		log.LogError("Error loading message: %s", err)
		return nil, err
	}
	return result, nil
}

func (mongo *MongoDB) Total() (int, error) {
	total, err := mongo.Messages.Find(bson.M{}).Count()
	if err != nil {
		log.LogError("Error loading message: %s", err)
		return -1, err
	}
	return total, nil
}

func (mongo *MongoDB) LoadAttachment(id string) (*Message, error) {
	result := &Message{}
	err := mongo.Messages.Find(bson.M{"attachments.id": id}).Select(bson.M{
		"id":            1,
		"attachments.$": 1,
	}).One(&result)
	if err != nil {
		log.LogError("Error loading attachment: %s", err)
		return nil, err
	}
	return result, nil
}

//Login validates and returns a user object if they exist in the database.
func (mongo *MongoDB) Login(username, password string) (*User, error) {
	u := &User{}
	err := mongo.Users.Find(bson.M{"username": username}).One(&u)
	if err != nil {
		log.LogError("Login error: %v", err)
		return nil, err
	}

	if ok := Validate_Password(u.Password, password); !ok {
		log.LogError("Invalid Password: %s", u.Username)
		return nil, fmt.Errorf("Invalid Password!")
	}

	return u, nil
}

func (mongo *MongoDB) StoreGreyHost(h *GreyHost) (string, error) {
	err := mongo.Hosts.Insert(h)
	if err != nil {
		log.LogError("Error inserting greylist ip: %s", err)
		return "", err
	}
	return h.Id.Hex(), nil
}

func (mongo *MongoDB) StoreGreyMail(m *GreyMail) (string, error) {
	err := mongo.Emails.Insert(m)
	if err != nil {
		log.LogError("Error inserting greylist email: %s", err)
		return "", err
	}
	return m.Id.Hex(), nil
}

func (mongo *MongoDB) IsGreyHost(hostname string) (int, error) {
	tl, err := mongo.Hosts.Find(bson.M{"hostname": hostname, "isactive": true}).Count()
	if err != nil {
		log.LogError("Error checking host greylist: %s", err)
		return -1, err
	}
	return tl, nil
}

func (mongo *MongoDB) IsGreyMail(email, t string) (int, error) {
	tl, err := mongo.Emails.Find(bson.M{"email": email, "type": t, "isactive": true}).Count()
	if err != nil {
		log.LogError("Error checking email greylist: %s", err)
		return -1, err
	}
	return tl, nil
}
