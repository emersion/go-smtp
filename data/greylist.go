package data

import (
	"time"

	"gopkg.in/mgo.v2/bson"
)

type GreyHost struct {
	Id        bson.ObjectId `bson:"_id"`
	CreatedBy string
	Hostname  string
	CreatedAt time.Time
	IsActive  bool
}

type GreyMail struct {
	Id        bson.ObjectId `bson:"_id"`
	CreatedBy string
	Type      string
	Email     string
	Local     string
	Domain    string
	CreatedAt time.Time
	IsActive  bool
}

type SpamIP struct {
	Id        bson.ObjectId `bson:"_id"`
	Hostname  string
	IPAddress string
	Type      string
	Email     string
	CreatedAt time.Time
	IsActive  bool
}
