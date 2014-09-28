package incus

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"
)

type CommandMsg struct {
	Command map[string]string      `json:"command"`
	Message map[string]interface{} `json:"message,omitempty"`
}

type Message struct {
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data"`
	Time  int64                  `json:"time"`
}

func (this *CommandMsg) FromSocket(sock *Socket) {
	command, ok := this.Command["command"]
	if !ok {
		return
	}

	if DEBUG {
		log.Printf("Handling socket message of type %s\n", command)
	}

	switch strings.ToLower(command) {
	case "message":
		if !CLIENT_BROAD {
			return
		}

		if sock.Server.Store.StorageType == "redis" {
			this.forwardToRedis(sock.Server)
			return
		}

		this.sendMessage(sock.Server)

	case "setpage":
		page, ok := this.Command["page"]
		if !ok || page == "" {
			return
		}

		if sock.Page != "" {
			sock.Server.Store.UnsetPage(sock) //remove old page if it exists
		}

		sock.Page = page
		sock.Server.Store.SetPage(sock) // set new page
	}
}

func (this *CommandMsg) FromRedis(server *Server) {
	command, ok := this.Command["command"]
	if !ok {
		return
	}

	if DEBUG {
		log.Printf("Handling redis message of type %s\n", command)
	}

	switch strings.ToLower(command) {

	case "message":
		this.sendMessage(server)
	}
}

func (this *CommandMsg) FromApp(server *Server) {
	command, ok := this.Command["command"]
	if !ok {
		if DEBUG {
			log.Printf("Missing command in message %s\n", command)
		}
		return
	}

	if DEBUG {
		log.Printf("Handling app message of type %s\n", command)
	}

	switch strings.ToLower(command) {

	case "message":
		this.sendMessage(server)
	}
}

func (this *CommandMsg) formatMessage() (*Message, error) {
	event, e_ok := this.Message["event"].(string)
	data, b_ok := this.Message["data"].(map[string]interface{})

	if !b_ok || !e_ok {
		if DEBUG {
			log.Printf("Could not format message")
		}
		return nil, errors.New("Could not format message")
	}

	msg := &Message{event, data, time.Now().UTC().Unix()}

	return msg, nil
}

func (this *CommandMsg) sendMessage(server *Server) {
	user, userok := this.Command["user"]
	page, pageok := this.Command["page"]

	if userok {
		this.messageUser(user, page, server)
	} else if pageok {
		this.messagePage(page, server)
	} else {
		this.messageAll(server)
	}
}

func (this *CommandMsg) messageUser(UID string, page string, server *Server) {
	msg, err := this.formatMessage()
	if err != nil {
		return
	}

	user, err := server.Store.Client(UID)
	if err != nil {
		return
	}

	for _, sock := range user {
		if page != "" && page != sock.Page {
			continue
		}

		if !sock.isClosed() {
			sock.buff <- msg
		}
	}
}

func (this *CommandMsg) messageAll(server *Server) {
	msg, err := this.formatMessage()
	if err != nil {
		return
	}

	clients := server.Store.Clients()

	for _, user := range clients {
		for _, sock := range user {
			if !sock.isClosed() {
				sock.buff <- msg
			}
		}
	}

	return
}

func (this *CommandMsg) messagePage(page string, server *Server) {
	msg, err := this.formatMessage()
	if err != nil {
		return
	}

	pageMap := server.Store.getPage(page)
	if pageMap == nil {
		return
	}

	for _, sock := range pageMap {
		if !sock.isClosed() {
			sock.buff <- msg
		}
	}

	return
}

func (this *CommandMsg) forwardToRedis(server *Server) {
	msg_str, _ := json.Marshal(this)
	server.Store.redis.Publish(server.Config.Get("redis_message_channel"), string(msg_str)) //pass the message into redis to send message across cluster
}
