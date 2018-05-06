package websockets

import (
	"time"

	"encoding/json"

	"github.com/gorilla/websocket"
	"github.com/mailhog/data"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
	// Maximum message size allowed from peer. Set to minimum allowed value as we don't expect the client to send non-control messages.
	maxMessageSize = 1
)

type connection struct {
	namespace string
	hub       *Hub
	ws        *websocket.Conn
	send      chan interface{}
}

func (c *connection) readLoop() {
	defer func() {
		c.hub.unregisterChan <- c
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		if _, _, err := c.ws.NextReader(); err != nil {
			return
		}
	}
}

func (c *connection) writeLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.writeControl(websocket.CloseMessage)
				return
			}

			msg, ok := message.(*data.Message)
			if !ok {
				return
			}

			ns := c.fetchNamespace(msg)
			if ns != c.namespace {
				return
			}

			if err := c.writeJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.writeControl(websocket.PingMessage); err != nil {
				return
			}
		}
	}
}

func (c *connection) fetchNamespace(msg *data.Message) string {
	xFields, ok := msg.Content.Headers["X-Fields"]
	if !ok && len(xFields) == 0 {
		return ""
	}

	xField := xFields[0]

	var xFieldJson struct {
		Microservice string `json:"ms"`
	}

	if err := json.Unmarshal([]byte(xField), &xFieldJson); err != nil {
		return ""
	}

	return xFieldJson.Microservice
}

func (c *connection) writeJSON(message interface{}) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteJSON(message)
}

func (c *connection) writeControl(messageType int) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(messageType, []byte{})
}
