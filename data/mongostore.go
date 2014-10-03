package data

import (
	"fmt"

	"github.com/gleez/smtpd/config"
	"github.com/gleez/smtpd/log"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
)

type MongoDB struct {
	Session    *mgo.Session
	Config     config.DataStoreConfig
	Collection *mgo.Collection
	Users      *mgo.Collection
	Hosts      *mgo.Collection
	Emails     *mgo.Collection
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
		Session:    session,
		Config:     c,
		Collection: session.DB(c.MongoDb).C(c.MongoColl),
		Users:      session.DB(c.MongoDb).C("Users"),
		Hosts:      session.DB(c.MongoDb).C("Hostgrey"),
		Emails:     session.DB(c.MongoDb).C("Emailgrey"),
	}
}

func (mongo *MongoDB) Close() {
	mongo.Session.Close()
}

func (mongo *MongoDB) Store(m *Message) (string, error) {
	err := mongo.Collection.Insert(m)
	if err != nil {
		log.LogError("Error inserting message: %s", err)
		return "", err
	}
	return m.Id, nil
}

func (mongo *MongoDB) List(start int, limit int) (*Messages, error) {
	messages := &Messages{}
	err := mongo.Collection.Find(bson.M{}).Sort("-_id").Skip(start).Limit(limit).Select(bson.M{
		"id":          1,
		"_id":         1,
		"from":        1,
		"to":          1,
		"attachments": 1,
		"created":     1,
		"ip":          1,
		"subject":     1,
	}).All(messages)
	if err != nil {
		log.LogError("Error loading messages: %s", err)
		return nil, err
	}
	return messages, nil
}

func (mongo *MongoDB) DeleteOne(id string) error {
	_, err := mongo.Collection.RemoveAll(bson.M{"id": id})
	return err
}

func (mongo *MongoDB) DeleteAll() error {
	_, err := mongo.Collection.RemoveAll(bson.M{})
	return err
}

func (mongo *MongoDB) Load(id string) (*Message, error) {
	result := &Message{}
	err := mongo.Collection.Find(bson.M{"id": id}).One(&result)
	if err != nil {
		log.LogError("Error loading message: %s", err)
		return nil, err
	}
	return result, nil
}

func (mongo *MongoDB) Total() (int, error) {
	//var total init
	total, err := mongo.Collection.Find(bson.M{}).Count()
	if err != nil {
		log.LogError("Error loading message: %s", err)
		return -1, err
	}
	return total, nil
}

func (mongo *MongoDB) LoadAttachment(id string) (*Message, error) {
	result := &Message{}
	err := mongo.Collection.Find(bson.M{"attachments.id": id}).Select(bson.M{
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
