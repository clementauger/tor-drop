build:
	go-bindata-assetfs assets/ templates/
	go build -tags prod -o tor-drop
clean:
	rm tor-drop
run:
	go run .
prod:
	go run -tags prod .
