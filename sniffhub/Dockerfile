docker build -t sniffhub -f deploy/Dockerfile .
docker run -d --restart=unless-stopped -p 8080:8080 --name sniffhub sniffhub
