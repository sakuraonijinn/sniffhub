BINARY=c2
PORT=443
CERT=fullchain.pem
KEY =privkey.pem

build:
	go build -trimpath -ldflags "-s -w" -o $(BINARY) .

run: build
	sudo ./$(BINARY) -cert $(CERT) -key $(KEY)

cloudflare:
	caddy reverse-proxy --from :80 --to :8080
