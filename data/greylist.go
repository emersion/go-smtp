package data

import (
	"time"

	"labix.org/v2/mgo/bson"
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
