package server

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	utils "github.com/johnietre/utils/go"
	webs "golang.org/x/net/websocket"
)

var (
	indexPath, password string
	msgs                = utils.NewSyncMap[string, []byte]()
	conns               = utils.NewSyncSet[*webs.Conn]()
)

func Run() {
	log.SetFlags(0)
	addr := flag.String("addr", "127.0.0.1:8000", "Address to listen on")
	flag.StringVar(
		&indexPath,
		"index", "static/html/index.html",
		"Path to index.html file",
	)
	cert := flag.String("cert", "", "Path to cert file")
	key := flag.String("key", "", "Path to key file")
	flag.Parse()

	if _, err := os.Stat(indexPath); err != nil {
		log.Fatal(err)
	}
	password = os.Getenv("MANYBOARDS_PASSWORD")
	if password == "" {
		log.Fatal("missing password... set using MANYBOARDS_PASSWORD")
	}
	if (*cert == "" && *key != "") || (*cert != "" && *key == "") {
		log.Fatal("if using TLS, must provide both cert and key paths")
	}

	http.HandleFunc("/", homeHandler)
	http.Handle("/ws", webs.Handler(wsHandler))
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal("error starting listener: ", err)
	}
	defer ln.Close()
	log.Print("serving on ", *addr)
	if *cert != "" {
		err = http.ServeTLS(ln, nil, *cert, *key)
	} else {
		err = http.Serve(ln, nil)
	}
	log.Fatal("error running server: ", err)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path != "" && path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, indexPath)
}

func wsHandler(ws *webs.Conn) {
	defer ws.Close()

	// Get the name
	name := ""
	for {
		var msg Message
		if err := webs.JSON.Receive(ws, &msg); err != nil {
			if utils.IsUnmarshalError(err) {
				webs.JSON.Send(ws, Message{Error: err.Error()})
				continue
			}
			return
		}
		msg.Name = strings.TrimSpace(msg.Name)
		if msg.Name == "" {
			webs.JSON.Send(ws, Message{Error: "must have a name"})
			continue
		} else if msg.Content != password {
			webs.JSON.Send(ws, Message{Error: "incorrect credentials"})
			continue
		}
		name = msg.Name
		break
	}
	webs.JSON.Send(ws, Message{Content: "OK"})

	defer conns.Remove(ws)
	conns.Insert(ws)
	msgs.Range(func(_ string, msgBytes []byte) bool {
		ws.Write(msgBytes)
		return true
	})

	// Listen for updates
	for {
		var msg Message
		if err := webs.JSON.Receive(ws, &msg); err != nil {
			if utils.IsUnmarshalError(err) {
				webs.JSON.Send(ws, Message{Error: err.Error()})
				continue
			}
			break
		}
		msg.Name, msg.Timestamp = name, time.Now().Unix()
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			webs.JSON.Send(ws, Message{Error: "server error: " + err.Error()})
			log.Printf("error marshaling message (%+v): %v", msg, err)
			continue
		}
		if msg.Delete {
			msgs.Delete(name)
		} else {
			msgs.Store(name, msgBytes)
		}
		conns.Range(func(conn *webs.Conn) bool {
			conn.Write(msgBytes)
			return true
		})
	}
}

type Message struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Hidden  bool   `json:"hidden,omitempty"`
	Delete  bool   `json:"delete,omitempty"`
	// Second precision
	Timestamp int64  `json:"timestamp,omitempty"`
	Error     string `json:"error,omitempty"`
}
