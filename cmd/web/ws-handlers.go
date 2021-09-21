package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

type WebSocketConnection struct {
	*websocket.Conn
}

type WsPayload struct {
	Action      string              `json:"action"`
	Message     string              `json:"message"`
	UserName    string              `json:"username"`
	MessageType string              `json:"message_type"`
	UserID      int                 `json:"user_id"`
	Conn        WebSocketConnection `json:"-"`
}

type WsJsonResponse struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	UserID  int    `json:"user_id"`
}

var upgradeConnection = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var clients = make(map[WebSocketConnection]string)

var wsChan = make(chan WsPayload)

// WsEndPoint upgrades a http connection to a websocket protocol upon request
// , and adds the ws client to the list of clients and register a listner
// for the websocket connection
func (app *application) WsEndPoint(w http.ResponseWriter, r *http.Request) {
	// upgrade the http connetion to websocket protocols
	ws, err := upgradeConnection.Upgrade(w, r, nil)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	app.infoLog.Println(fmt.Sprintf("Client connected from %s", r.RemoteAddr))

	var response WsJsonResponse
	response.Message = "Connected to server"

	err = ws.WriteJSON(response)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// add the websocket connection to clients slice
	conn := WebSocketConnection{Conn: ws}
	clients[conn] = ""

	// listen for the connection forever
	go app.ListenForWS(&conn)
}

// ListenForWS reads a payload from a websocket client
// and then sends it to a websocket channel
func (app *application) ListenForWS(conn *WebSocketConnection) {
	defer func() {
		if r := recover(); r != nil {
			app.errorLog.Println("ERROR:", fmt.Sprintf("%v", r))
		}
	}()

	var payload WsPayload

	// read the payload from the websocket client and send it to ws channel
	for {
		err := conn.ReadJSON(&payload)
		if err != nil {
			// do nothing
		} else {
			payload.Conn = *conn
			wsChan <- payload
		}
	}
}

// ListenToWsChannel listens to websocket channel and constructs
// a json response to be broadcast to all websocket clients
func (app *application) ListenToWsChannel() {
	var response WsJsonResponse

	// ws server listens to the ws channel and if ws receives 'deleteUser' action
	// from the ws client, construct ws response and broadcast it to
	// all ws clients
	for {
		e := <-wsChan
		switch e.Action {
		case "deleteUser":
			response.Action = "logout"
			response.Message = "Your account has been deleted"
			response.UserID = e.UserID
			app.broadcastToAll(response)
		default:
		}
	}
}

// broadcastToAll sends a ws response to all ws clients
func (app *application) broadcastToAll(response WsJsonResponse) {
	for client := range clients {
		// broadcast the response to every connected client
		err := client.WriteJSON(response)
		if err != nil {
			app.errorLog.Printf("Websocket err on %s: %s", response.Action, err)
			_ = client.Close()
			delete(clients, client)
		}
	}
}
