package main

import (
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"encoding/json"
)

type Hub struct {
	register    chan *Conn
	unregister  chan *Conn
	connections map[*Conn]bool
	yo          chan *Yo
}

type Yo struct {
	From string `json:"from"`
}

type Conn struct {
	ws     *websocket.Conn
	outbox chan []byte
}

var (
	upgrader = &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	hub = &Hub{
		register:    make(chan *Conn),
		unregister:  make(chan *Conn),
		yo:          make(chan *Yo, 1024),
		connections: make(map[*Conn]bool),
	}
	addr = flag.String("addr", ":80", "http service address")
)

func (conn *Conn) write() {
	for msg := range conn.outbox {
		err := conn.ws.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			break
		}
	}
	conn.ws.Close()
}

func (conn *Conn) kill() {
	hub.unregister <- conn
}

func (hub *Hub) run() {
	for {
		select {
		// There is a connection 'c' in the registration queue
		case conn := <-hub.register:
			hub.connections[conn] = true
		// There is a connection 'c' in the unregistration queue
		case conn := <-hub.unregister:
			delete(hub.connections, conn)
			close(conn.outbox)
			conn.ws.Close()
		case yo := <-hub.yo:
			log.Println("yo", yo.From)
			go hub.notify(yo)
		}
	}
}

func (hub *Hub) notify(yo *Yo) {
	msg, err := json.Marshal(yo)
	if err == nil {
		for conn, _ := range hub.connections {
			conn.outbox <- msg
		}
	} else {
		log.Println("Could not marshal json:", err)
	}
}

func wsHandler(res http.ResponseWriter, req *http.Request) {
	log.Println("ws")
	ws, err := upgrader.Upgrade(res, req, nil)
	if err != nil {
		log.Println("Could not upgrade incoming connection", err)
		return
	}
	conn := &Conn{
		outbox: make(chan []byte, 256),
		ws:     ws,
	}
	hub.register <- conn
	defer conn.kill() // Kill the connection on exit
	conn.write()      // Left outside go routine to stall execution
}

func main() {
	// Register turbo handler
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/flappyo", func(w http.ResponseWriter, r *http.Request) {
		yo := Yo{}
		yo.From = r.URL.Query().Get("username")
		hub.yo <- &yo
		log.Println("Flap!")
		fmt.Fprintf(w, "Flap!")
	})
	// Register the static files
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		http.ServeFile(res, req, "./static/index.html")
	})
	// Start the server
	go hub.run()
	log.Println("Server is now listening on 0.0.0.0")
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal("Server could not start:", err)
	}
}
