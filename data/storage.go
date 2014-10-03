package data

import (
	"fmt"
	"io"

	"github.com/gleez/smtpd/config"
	"github.com/gleez/smtpd/log"
)

type DataStore struct {
	Config         config.DataStoreConfig
	Storage        interface{}
	SaveMailChan   chan *config.SMTPMessage
	NotifyMailChan chan interface{}
}

// DefaultDataStore creates a new DataStore object.
func NewDataStore() *DataStore {
	cfg := config.GetDataStoreConfig()

	// Database Writing
	saveMailChan := make(chan *config.SMTPMessage, 256)

	// Websocket Notification
	notifyMailChan := make(chan interface{}, 256)

	return &DataStore{Config: cfg, SaveMailChan: saveMailChan, NotifyMailChan: notifyMailChan}
}

func (ds *DataStore) StorageConnect() {

	if ds.Config.Storage == "mongodb" {
		log.LogInfo("Trying MongoDB storage")
		s := CreateMongoDB(ds.Config)
		if s == nil {
			log.LogInfo("MongoDB storage unavailable")
		} else {
			log.LogInfo("Using MongoDB storage")
			ds.Storage = s
		}

		// start some savemail workers
		for i := 0; i < 3; i++ {
			go ds.SaveMail()
		}
	}
}

func (ds *DataStore) StorageDisconnect() {
	if ds.Config.Storage == "mongodb" {
		ds.Storage.(*MongoDB).Close()
	}
}

func (ds *DataStore) SaveMail() {
	log.LogTrace("Running SaveMail Rotuines")
	var err error
	var recon bool

	for {
		mc := <-ds.SaveMailChan
		msg := ParseSMTPMessage(mc, mc.Domain, ds.Config.MimeParser)

		if ds.Config.Storage == "mongodb" {
			mc.Hash, err = ds.Storage.(*MongoDB).Store(msg)

			// if mongo conection is broken, try to reconnect only once
			if err == io.EOF && !recon {
				log.LogWarn("Connection error trying to reconnect")
				ds.Storage = CreateMongoDB(ds.Config)
				recon = true

				//try to save again
				mc.Hash, err = ds.Storage.(*MongoDB).Store(msg)
			}

			if err == nil {
				recon = false
				log.LogTrace("Save Mail Client hash : <%s>", mc.Hash)
				mc.Notify <- 1

				//Notify web socket
				ds.NotifyMailChan <- mc.Hash
			} else {
				mc.Notify <- -1
				log.LogError("Error storing message: %s", err)
			}
		}
	}
}

// Check if host address is in greylist
// h -> hostname client ip
func (ds *DataStore) HostGreyList(h string) bool {
	to, err := ds.Storage.(*MongoDB).IsGreyHost(h)
	if err != nil {
		return false
	}

	return to > 0
}

// Check if email address is in greylist
// t -> type (from/to)
// m -> local mailbox
// d -> domain
// h -> client IP
func (ds *DataStore) MailGreyList(t, m, d, h string) bool {
	e := fmt.Sprintf("%s@%s", m, d)
	to, err := ds.Storage.(*MongoDB).IsGreyMail(e, t)
	if err != nil {
		return false
	}

	return to > 0
}
