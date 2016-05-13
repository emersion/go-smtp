package smtp

import (
	"fmt"
	"io"
	"time"
)

type DataStore struct {
	SaveMailChan chan *config.SMTPMessage
}

// DefaultDataStore creates a new DataStore object.
func NewDataStore() *DataStore {
	cfg := config.GetDataStoreConfig()

	// Database Writing
	saveMailChan := make(chan *config.SMTPMessage, 256)

	return &DataStore{Config: cfg, SaveMailChan: saveMailChan}
}

func (ds *DataStore) SaveMail() {
	for {
		mc := <-ds.SaveMailChan
		// TODO
	}
}
